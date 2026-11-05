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
type Exercise struct {
	ID                              uint
	Name                            string `gorm:"uniqueIndex"`
	Muscle, Equipment, Guide, Setup string
	AuxMuscles, ImageURL, Mistakes  string
}
type Session struct {
	ID, Owner, PlanID uint
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
func login(c *gin.Context) {
	var in struct{ Code string }
	if c.ShouldBindJSON(&in) != nil || in.Code == "" {
		fail(c, 400, "缺少微信凭证")
		return
	}
	q := url.Values{"appid": {os.Getenv("WX_APP_ID")}, "secret": {os.Getenv("WX_SECRET")}, "grant_type": {"authorization_code"}, "js_code": {in.Code}}
	client := http.Client{Timeout: 8 * time.Second}
	req, _ := http.NewRequestWithContext(c.Request.Context(), "GET", "https://api.weixin.qq.com/sns/jscode2session?"+q.Encode(), nil)
	res, e := client.Do(req)
	if e != nil {
		fail(c, 502, "微信服务不可用")
		return
	}
	defer res.Body.Close()
	var w struct{ OpenID string }
	if json.NewDecoder(res.Body).Decode(&w) != nil || w.OpenID == "" {
		fail(c, 401, "微信登录失败")
		return
	}
	u := User{OpenID: w.OpenID}
	if db.Clauses(clause.OnConflict{DoNothing: true}).Create(&u).Error != nil || db.Where("open_id = ?", w.OpenID).First(&u).Error != nil {
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
	api.GET("/exercises", func(c *gin.Context) { var rows []Exercise; e := db.Order("id").Find(&rows).Error; save(c, rows, e) })
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
	srv := http.Server{Addr: ":18080", Handler: r, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, BaseContext: nil}
	log.Fatal(srv.ListenAndServe())
}
