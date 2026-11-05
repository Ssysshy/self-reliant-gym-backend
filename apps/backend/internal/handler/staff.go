package handler

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm/clause"
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
	c.SetSameSite(3)
	c.SetCookie("gym_admin", id, 43200, "/api/admin", "", os.Getenv("GYM_LOCAL") != "1", true)
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
