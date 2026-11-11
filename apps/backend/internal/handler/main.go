package handler

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gym/migrations"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type User struct {
	ID         uint
	OpenID     string `gorm:"uniqueIndex" json:"-"`
	Experience string
	Active     uint
	Expires    time.Time `gorm:"type:datetime;default:null"`
}
type Item struct {
	Name             string
	Sets, Reps, Rest int
}
type Day struct {
	Weekday int
	Focus   string
	Items   []Item
}
type Plan struct {
	OperationID *string `json:"-"`
	ID          uint
	Owner       uint `gorm:"index"`
	Name        string
	Days        []Day `gorm:"serializer:json;type:json"`
	Published   bool
	Revision    uint
}

// Exercise 是动作库里的一条动作。Muscle 是左侧部位（胸/背/肩…），SubMuscle 是同部位内的细分
// （上胸/中胸/下胸…，叶子部位留空），Category 是顶部筛选用的器材分类（器械/杠铃/哑铃…），
// Equipment 才是具体器材名，套餐生成和训练计划按器材名匹配。
type Exercise struct {
	ID                                     uint
	Name                                   string `gorm:"uniqueIndex"`
	Muscle, SubMuscle, Category, Equipment string
	Guide, Setup                           string
	AuxMuscles, ImageURL, Mistakes         string
}
type Session struct {
	ID, Owner, PlanID uint
	OperationID       *string    `json:"-"`
	Day               Day        `gorm:"serializer:json;type:json"`
	Done              []int      `gorm:"serializer:json;type:json"`
	Weights           []*float64 `gorm:"serializer:json;type:json"`
	Started           time.Time
	Ended             *time.Time
}
type Grant struct {
	Revoked          *time.Time
	ID, UserID       uint
	Days             int
	Reason, Operator string
	CreatedAt        time.Time
}

var db *gorm.DB
var key []byte

func required(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatal("缺少配置 ", k)
	}
	return v
}
func fail(c *gin.Context, status int, msg string) { c.AbortWithStatusJSON(status, gin.H{"error": msg}) }
func uid(c *gin.Context) uint                     { return c.MustGet("uid").(uint) }
func token(id uint, role string) string {
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": strconv.FormatUint(uint64(id), 10), "aud": role, "exp": time.Now().Add(12 * time.Hour).Unix()})
	s, e := t.SignedString(key)
	if e != nil {
		panic(e)
	}
	return s
}
func auth(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		t, e := jwt.Parse(raw, func(_ *jwt.Token) (any, error) { return key, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience(role), jwt.WithExpirationRequired())
		if e != nil {
			fail(c, 401, "请登录")
			return
		}
		sub, _ := t.Claims.GetSubject()
		id, e := strconv.ParseUint(sub, 10, 32)
		if e != nil {
			fail(c, 401, "登录过期")
			return
		}
		c.Set("uid", uint(id))
		c.Next()
	}
}
func save(c *gin.Context, v any, e error) {
	if errors.Is(e, gorm.ErrRecordNotFound) {
		fail(c, 404, "记录不存在")
		return
	}
	if e != nil {
		fail(c, 500, "操作失败")
		return
	}
	c.JSON(200, v)
}

// openid 用小程序 code 换取用户唯一标识。
// 微信未返回 errcode 时只能看到一句「登录失败」，所以把 errcode 打进日志，便于区分凭证配错与 code 失效。
// 本地开发（GYM_LOCAL=1）且未配置密钥时退化为固定测试账号，便于离线跑通完整流程。
func openid(c *gin.Context, code string) (string, error) {
	if os.Getenv("GYM_LOCAL") == "1" && os.Getenv("WX_SECRET") == "" {
		if v := os.Getenv("WX_MOCK_OPENID"); v != "" {
			return v, nil
		}
		return "local_dev_user", nil
	}
	q := url.Values{"appid": {os.Getenv("WX_APP_ID")}, "secret": {os.Getenv("WX_SECRET")}, "grant_type": {"authorization_code"}, "js_code": {code}}
	client := http.Client{Timeout: 8 * time.Second}
	req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", "https://api.weixin.qq.com/sns/jscode2session?"+q.Encode(), nil)
	res, e := client.Do(req)
	if e != nil {
		return "", errors.New("微信服务不可用")
	}
	defer res.Body.Close()
	var w struct {
		OpenID, ErrMsg string
		ErrCode        int
	}
	if e = json.NewDecoder(res.Body).Decode(&w); e != nil {
		return "", e
	}
	if w.OpenID == "" {
		log.Printf("jscode2session 失败 errcode=%d errmsg=%q appid=%t secret=%t", w.ErrCode, w.ErrMsg, os.Getenv("WX_APP_ID") != "", os.Getenv("WX_SECRET") != "")
		return "", errors.New("微信登录失败")
	}
	return w.OpenID, nil
}
func login(c *gin.Context) {
	var in struct{ Code string }
	if c.ShouldBindJSON(&in) != nil || in.Code == "" {
		fail(c, 400, "缺少微信凭证")
		return
	}
	openID, e := openid(c, in.Code)
	if e != nil {
		fail(c, 401, e.Error())
		return
	}
	u := User{OpenID: openID}
	if db.Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error != nil || db.Where("open_id = ?", openID).First(&u).Error != nil {
		fail(c, 500, "登录失败")
		return
	}
	c.JSON(200, gin.H{"token": token(u.ID, "user"), "user": u})
}
func Run() {
	key = []byte(required("SESSION_SECRET"))
	if len(key) < 32 {
		log.Fatal("SESSION_SECRET 至少 32 字节")
	}
	if os.Getenv("WX_SECRET") == "" {
		if os.Getenv("GYM_LOCAL") == "1" {
			log.Println("未配置 WX_SECRET：微信登录改为固定测试账号 local_dev_user（WX_MOCK_OPENID 可覆盖）")
		} else {
			log.Println("未配置 WX_SECRET：微信登录将不可用")
		}
	}
	admin := required("ADMIN_ACCOUNT")
	password := required("ADMIN_PASSWORD")
	var e error
	db, e = gorm.Open(mysql.Open(required("MYSQL_DSN")))
	if e != nil {
		log.Fatal(e)
	}
	if e = migrations.Run(db); e != nil {
		log.Fatal(e)
	}
	seed()
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api/v1")
	api.POST("/auth/wechat", login)
	registerCatalog(api)
	u := api.Group("", auth("user"))
	u.GET("/me", func(c *gin.Context) { var v User; e := db.First(&v, uid(c)).Error; save(c, v, e) })
	u.PUT("/experience", func(c *gin.Context) {
		var v struct{ Experience string }
		if c.ShouldBindJSON(&v) != nil || (v.Experience != "小白" && v.Experience != "训练老炮") {
			fail(c, 400, "请选择经验")
			return
		}
		save(c, v, db.Model(&User{}).Where("id = ?", uid(c)).Update("experience", v.Experience).Error)
	})
	api.GET("/billing/notifications/wechat", notifications)
	api.POST("/billing/notifications/wechat", notifications)
	u.POST("/orders", purchase)
	u.GET("/orders", orders)
	accountRoutes(u)
	registerPlans(u)
	u.POST("/plans/generate", generate)
	registerTraining(u)
	a := r.Group("/api/admin/v1")
	if e = bootstrap(admin, password, true); e != nil {
		log.Fatal(e)
	}
	if e = bootstrap(os.Getenv("OPERATOR_ACCOUNT"), os.Getenv("OPERATOR_PASSWORD"), false); e != nil {
		log.Fatal(e)
	}
	a.POST("/login", staffLogin)
	a.Use(staffAuth)
	a.GET("/me", func(c *gin.Context) { c.JSON(200, gin.H{"admin": c.GetBool("admin")}) })
	a.POST("/orders/:id/reconcile", reconcile)
	u.POST("/orders/:id/reconcile", reconcile)
	a.GET("/orders", orders)
	catalogProducts(u, a)
	staffGrants(a)
	registerAdmin(a)
	go watchOrders()
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":18080"
	}
	srv := http.Server{Addr: addr, Handler: r, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, BaseContext: nil}
	log.Fatal(srv.ListenAndServe())
}
