package handler

import (
	"github.com/gin-gonic/gin"
	"time"
)

type Feedback struct {
	ID, Owner uint
	Content   string `gorm:"size:1000"`
	Status    string `gorm:"size:16"`
	HandledBy *string
	Note      *string `gorm:"size:500"`
	HandledAt *time.Time
	CreatedAt time.Time
}

func accountRoutes(g *gin.RouterGroup) {
	g.POST("/feedback", func(c *gin.Context) {
		var in struct{ Content string }
		if c.ShouldBindJSON(&in) != nil || len([]rune(in.Content)) < 1 || len([]rune(in.Content)) > 1000 {
			fail(c, 400, "反馈须为 1 至 1000 字")
			return
		}
		save(c, true, db.Create(&Feedback{Owner: uid(c), Content: in.Content, Status: "待处理"}).Error)
	})
	g.GET("/training/summary", func(c *gin.Context) {
		var out struct{ Total, Month int64 }
		now := time.Now().In(time.FixedZone("北京", 28800))
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UTC()
		e := db.Raw("SELECT COUNT(*) AS total, COALESCE(SUM(started>=?),0) AS month FROM sessions WHERE owner=? AND ended IS NOT NULL AND EXISTS (SELECT 1 FROM JSON_TABLE(done,'$[*]' COLUMNS(reps INT PATH '$')) AS completed WHERE completed.reps>0)", start, uid(c)).Scan(&out).Error
		save(c, out, e)
	})
}

func (Feedback) TableName() string { return "feedbacks" }
