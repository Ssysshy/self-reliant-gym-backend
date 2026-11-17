package handler

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"os"
	"strings"
	"time"
)

type Staff struct {
	ID      uint
	Login   string `gorm:"size:96;uniqueIndex"`
	Hash    string `json:"-"`
	Admin   bool
	Enabled bool
}
type StaffSession struct {
	ID      string `gorm:"primaryKey;size:64"`
	StaffID uint
	Until   time.Time
}

func bootstrap(login, password string, admin bool) error {
	if login == "" || password == "" {
		return nil
	}
	var count int64
	if err := db.Model(&Staff{}).Where("login=?", login).Count(&count).Error; err != nil || count > 0 {
		return err
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if e != nil {
		return e
	}
	return db.Clauses(clause.OnConflict{DoNothing: true}).Create(&Staff{Login: login, Hash: string(hash), Admin: admin, Enabled: true}).Error
}
func staffLogin(c *gin.Context) {
	var in struct{ Account, Password string }
	var staff Staff
	if c.GetHeader("X-Gym-Admin") != "1" || c.ShouldBindJSON(&in) != nil || db.Where("login=? AND enabled=true", in.Account).First(&staff).Error != nil || bcrypt.CompareHashAndPassword([]byte(staff.Hash), []byte(in.Password)) != nil {
		fail(c, 401, "账号或密码错误")
		return
	}
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		fail(c, 500, "登录失败")
		return
	}
	id := hex.EncodeToString(b[:])
	s := StaffSession{ID: sign(string(key), id), StaffID: staff.ID, Until: time.Now().Add(12 * time.Hour)}
	if e := db.Create(&s).Error; e != nil {
		save(c, nil, e)
		return
	}
	// cookie 的 Secure 属性决定浏览器是否在明文 HTTP 下保存它：
	// 生产走 HTTPS 时必须为 true；但此时若只有一个公网 IP、没法上证书，
	// 浏览器会静默丢弃这个 cookie，表现为「登录返回成功，下一个接口却要重新登录」。
	// 所以给它一个显式开关，默认仍按老逻辑（非 GYM_LOCAL 即要求 HTTPS）。
	secure := os.Getenv("GYM_LOCAL") != "1"
	if v := os.Getenv("COOKIE_SECURE"); v != "" {
		secure = v == "1"
	}
	// SameSite=None 必须搭配 Secure，否则同样会被浏览器拒绝；
	// 降级到明文时退回 Lax —— 后台页与 /api 同源，Lax 足够。
	sameSite := http.SameSiteLaxMode
	if secure {
		sameSite = http.SameSiteNoneMode
	}
	c.SetSameSite(sameSite)
	c.SetCookie("gym_admin", id, 43200, "/api/admin", "", secure, true)
	c.JSON(200, true)
}
func staffAuth(c *gin.Context) {
	cookie, e := c.Cookie("gym_admin")
	if e != nil || cookie == "" {
		fail(c, 401, "请登录")
		return
	}
	sid := sign(string(key), cookie)
	var staff Staff
	if db.Table("staffs").Select("staffs.*").Joins("JOIN staff_sessions ON staff_sessions.staff_id=staffs.id").Where("staff_sessions.id=? AND staff_sessions.`until`>? AND enabled=true", sid, time.Now()).Take(&staff).Error != nil {
		fail(c, 401, "请登录")
		return
	}
	if c.Request.Method != "GET" && c.GetHeader("X-Gym-Admin") != "1" {
		fail(c, 403, "来源无效")
		return
	}
	path := c.FullPath()
	if !staff.Admin && (strings.Contains(path, "/grants") || strings.Contains(path, "/audit") || (strings.Contains(path, "/products") && c.Request.Method != "GET")) {
		fail(c, 403, "需要管理员权限")
		return
	}
	c.Set("actor", staff.Login)
	c.Set("admin", staff.Admin)
	c.Set("sid", sid)
	c.Set("uid", staff.ID)
	c.Next()
}

func (Staff) TableName() string { return "staffs" }

// changePassword 改当前登录账号的密码。
// 旧密码校验通过才写新哈希；同时清掉这个账号的其他 session ——
// 密码改了、旧登录态还能用 12 小时，等于没改。当前这条 session 保留，
// 免得改完立刻被踢出去。
func changePassword(c *gin.Context) {
	var in struct{ Old, New string }
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "参数错误")
		return
	}
	// bcrypt 只处理前 72 字节，超长会被静默截断，所以显式挡住
	if n := len([]byte(in.New)); n < 8 || n > 72 {
		fail(c, 400, "新密码长度需在 8 到 72 字节之间")
		return
	}
	if in.Old == in.New {
		fail(c, 400, "新密码不能与原密码相同")
		return
	}
	var staff Staff
	if e := db.First(&staff, c.GetUint("uid")).Error; e != nil {
		fail(c, 401, "请登录")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(staff.Hash), []byte(in.Old)) != nil {
		fail(c, 400, "原密码不正确")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.New), bcrypt.DefaultCost)
	if e != nil {
		fail(c, 500, "修改失败")
		return
	}
	e = db.Transaction(func(tx *gorm.DB) error {
		if e := tx.Model(&Staff{}).Where("id=?", staff.ID).Update("hash", string(hash)).Error; e != nil {
			return e
		}
		return tx.Where("staff_id=? AND id<>?", staff.ID, c.GetString("sid")).Delete(&StaffSession{}).Error
	})
	if e != nil {
		save(c, nil, e)
		return
	}
	c.JSON(200, true)
}
