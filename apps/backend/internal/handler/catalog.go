package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
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

// imagePath 校验示范图地址。库里只存相对路径（/assets/exercises/x.png），
// 域名由前端的 CDN 前缀统一拼，换 CDN 不用改数据；老数据里的 https 完整链接照样收。
// http 不给过：正式版真机上 <image> 会被微信拦，填了也显示不出来。
func imagePath(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" || strings.HasPrefix(v, "https://") {
		return true
	}
	return !strings.Contains(v, "://")
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

// exerciseBrief 是列表接口下发的字段：列表页只用得到名称、部位、器材和缩略图。
// 教学文案（Guide/Setup/Mistakes）每条上百字，动作库涨到 878 条时全量下发会到几百 KB，
// 而列表页一个字段都用不上——详情页单独走 /exercises/:id 取全量。
type exerciseBrief struct {
	ID        uint
	Name      string
	Muscle    string
	SubMuscle string
	Category  string
	Equipment string
	ImageURL  string
}

// 列表分页：pageNo 从 1 开始，pageSize 默认 20、硬上限 200。
// 只暴露页号和页大小，不暴露 offset——调用方不需要知道偏移怎么算。
const (
	exercisePageSizeDefault = 20
	exercisePageSizeMax     = 200
)

// parsePage 解析 pageNo/pageSize。写错、负数、0、超上限都归一到合法区间：
// 一个查询参数不该让整个请求 500，也不该被 pageSize=99999 绕过。
func parsePage(rawNo, rawSize string) (pageNo, pageSize int) {
	pageNo, pageSize = 1, exercisePageSizeDefault
	if v, e := strconv.Atoi(rawNo); e == nil && v > 0 {
		pageNo = v
	}
	if v, e := strconv.Atoi(rawSize); e == nil && v > 0 {
		pageSize = v
	}
	if pageSize > exercisePageSizeMax {
		pageSize = exercisePageSizeMax
	}
	return pageNo, pageSize
}

func exerciseList(c *gin.Context) {
	muscle, sub, category, keyword := c.Query("muscle"), c.Query("sub"), c.Query("category"), c.Query("q")
	pageNo, pageSize := parsePage(c.Query("pageNo"), c.Query("pageSize"))

	var total int64
	if e := exerciseQuery(muscle, sub, category, keyword).Model(&Exercise{}).Count(&total).Error; e != nil {
		save(c, nil, e)
		return
	}
	rows := []exerciseBrief{}
	e := exerciseQuery(muscle, sub, category, keyword).Model(&Exercise{}).
		Select("id, name, muscle, sub_muscle, category, equipment, image_url").
		Limit(pageSize).Offset((pageNo - 1) * pageSize).Find(&rows).Error
	if e != nil {
		save(c, nil, e)
		return
	}
	c.JSON(200, gin.H{"Items": rows, "Total": total, "PageNo": pageNo, "PageSize": pageSize})
}

// adminExerciseList 给后台表格用：编辑表单要 Guide/Setup/Mistakes 全字段，故不复用精简版。
// 分页口径与小程序侧一致（pageNo/pageSize，默认 20）。
func adminExerciseList(c *gin.Context) {
	muscle, sub, category, keyword := c.Query("muscle"), c.Query("sub"), c.Query("category"), c.Query("q")
	pageNo, pageSize := parsePage(c.Query("pageNo"), c.Query("pageSize"))

	var total int64
	if e := exerciseQuery(muscle, sub, category, keyword).Model(&Exercise{}).Count(&total).Error; e != nil {
		save(c, nil, e)
		return
	}
	rows := []Exercise{}
	e := exerciseQuery(muscle, sub, category, keyword).
		Limit(pageSize).Offset((pageNo - 1) * pageSize).Find(&rows).Error
	if e != nil {
		save(c, nil, e)
		return
	}
	c.JSON(200, gin.H{"Items": rows, "Total": total, "PageNo": pageNo, "PageSize": pageSize})
}

func registerCatalog(g *gin.RouterGroup) {
	g.GET("/exercises", exerciseList)
	g.GET("/exercises/:id", func(c *gin.Context) {
		var x Exercise
		save(c, x, db.First(&x, "id = ?", c.Param("id")).Error)
	})
	g.GET("/categories", categories)
}
