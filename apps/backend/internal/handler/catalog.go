package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"strings"
)

// Part 是动作库左侧的一个部位；有细分的部位展开成两行，没有细分的就是叶子。
// 顺序即小程序侧边栏顺序，前后台共用这份定义，避免两边各写一套。
type Part struct {
	Name string
	Subs []string
}

var catalogParts = []Part{
	{Name: "胸", Subs: []string{"上胸", "中胸", "下胸"}},
	{Name: "背", Subs: []string{"背阔肌", "中背", "下背"}},
	{Name: "肩", Subs: []string{"前束", "中束", "后束"}},
	{Name: "二头", Subs: []string{}},
	{Name: "三头", Subs: []string{}},
	{Name: "小臂", Subs: []string{}},
	{Name: "腿", Subs: []string{"股四头", "腘绳肌"}},
	{Name: "小腿", Subs: []string{}},
	{Name: "臀部", Subs: []string{}},
	{Name: "腹部", Subs: []string{}},
	{Name: "斜方肌", Subs: []string{}},
	{Name: "有氧", Subs: []string{}},
}

var catalogCategories = []string{"器械", "杠铃", "哑铃", "史密斯", "绳索", "弹力带", "徒手"}

func categories(c *gin.Context) {
	c.JSON(200, gin.H{"Parts": catalogParts, "Categories": catalogCategories})
}

// taxonomy 校验部位、细分与器材分类是否在约定范围内。
// 细分允许留空（叶子部位或还没细分的动作），但不能挂在别的部位名下。
func taxonomy(x Exercise) string {
	var subs []string
	found := false
	for _, p := range catalogParts {
		if p.Name == x.Muscle {
			found, subs = true, p.Subs
			break
		}
	}
	if !found {
		return "请选择部位"
	}
	if x.SubMuscle != "" && len(subs) > 0 && !contains(subs, x.SubMuscle) {
		return "细分肌群不属于该部位"
	}
	if len(subs) == 0 && x.SubMuscle != "" {
		return "该部位没有细分肌群"
	}
	if !contains(catalogCategories, x.Category) {
		return "请选择器材分类"
	}
	return ""
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// exerciseQuery 把列表筛选条件翻译成 SQL：部位、细分、器材分类和关键词互不干扰。
func exerciseQuery(muscle, sub, category, keyword string) *gorm.DB {
	q := db.Order("id")
	if v := strings.TrimSpace(muscle); v != "" {
		q = q.Where("muscle = ?", v)
	}
	if v := strings.TrimSpace(sub); v != "" {
		q = q.Where("sub_muscle = ?", v)
	}
	if v := strings.TrimSpace(category); v != "" {
		q = q.Where("category = ?", v)
	}
	if v := strings.TrimSpace(keyword); v != "" {
		like := "%" + v + "%"
		q = q.Where("name LIKE ? OR muscle LIKE ? OR sub_muscle LIKE ? OR equipment LIKE ?", like, like, like, like)
	}
	return q
}

func exerciseList(c *gin.Context) {
	var rows []Exercise
	q := exerciseQuery(c.Query("muscle"), c.Query("sub"), c.Query("category"), c.Query("q"))
	save(c, rows, q.Find(&rows).Error)
}

func registerCatalog(g *gin.RouterGroup) {
	g.GET("/exercises", exerciseList)
	g.GET("/exercises/:id", func(c *gin.Context) {
		var x Exercise
		save(c, x, db.First(&x, "id = ?", c.Param("id")).Error)
	})
	g.GET("/categories", categories)
}
