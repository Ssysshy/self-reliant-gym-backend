package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"os"
	"strconv"
)

type Product struct {
	Days    int `gorm:"primaryKey;autoIncrement:false"`
	Price   int
	Enabled bool
}

func catalogProducts(g, a *gin.RouterGroup) {
	for _, p := range []Product{{7, 990, true}, {30, 2990, true}, {365, 12800, true}} {
		if e := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; e != nil {
			panic(e)
		}
	}
	a.GET("/products", func(c *gin.Context) { var v []Product; e := db.Order("days").Find(&v).Error; save(c, v, e) })
	a.PUT("/products/:days", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var in struct{ Enabled *bool }
		if c.ShouldBindJSON(&in) != nil || in.Enabled == nil {
			fail(c, 400, "参数无效")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var p Product
			if err := tx.First(&p, "days=?", c.Param("days")).Error; err != nil {
				return err
			}
			if err := tx.Model(&p).Update("enabled", *in.Enabled).Error; err != nil {
				return err
			}
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: "套餐上架状态 " + strconv.FormatBool(*in.Enabled), Target: uint(p.Days)}).Error
		})
		save(c, true, e)
	})
	g.GET("/products", func(c *gin.Context) {
		var v []Product
		if e := db.Where("enabled=true").Order("days").Find(&v).Error; e != nil {
			save(c, nil, e)
			return
		}
		for i, p := range v {
			v[i].Enabled = os.Getenv("XPAY_PRODUCT_"+strconv.Itoa(p.Days)) != "" && os.Getenv("XPAY_KEY") != "" && os.Getenv("WX_PUSH_AES") != "" && os.Getenv("WX_PUSH_TOKEN") != "" && os.Getenv("XPAY_OFFER") != ""
		}
		c.JSON(200, v)
	})
}
