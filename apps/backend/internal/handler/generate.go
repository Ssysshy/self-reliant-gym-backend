package handler

import (
	"github.com/gin-gonic/gin"
	"time"
)

// generate builds an editable weekly draft from published plans and the user's available equipment.
func generate(c *gin.Context) {
	var input struct {
		Weekdays  []int
		Equipment []string
		Minutes   int
		Focus     string
	}
	if c.ShouldBindJSON(&input) != nil || len(input.Weekdays) < 1 || len(input.Weekdays) > 7 || input.Minutes < 5 || input.Minutes > 180 {
		fail(c, 400, "请检查训练日和时长")
		return
	}
	var user User
	if e := db.First(&user, uid(c)).Error; e != nil {
		save(c, nil, e)
		return
	}
	if !user.Expires.After(time.Now()) {
		fail(c, 403, "智能生成需要有效会员")
		return
	}
	allowed := make(map[string]bool)
	for _, v := range input.Equipment {
		allowed[v] = true
	}
	var exercises []Exercise
	if e := db.Order("id").Find(&exercises).Error; e != nil {
		save(c, nil, e)
		return
	}
	var plan Plan
	plan.Name = input.Focus + "周计划"
	seen := map[int]bool{}
	for _, weekday := range input.Weekdays {
		if weekday < 1 || weekday > 7 || seen[weekday] {
			fail(c, 400, "训练日无效")
			return
		}
		seen[weekday] = true
		day := Day{Weekday: weekday, Focus: input.Focus}
		seconds := input.Minutes * 60
		for _, x := range exercises {
			// 可用器材按「分类或具体器材」匹配：小程序让用户勾的是 器械/杠铃/哑铃 这类分类，
			// 早期按具体器材名存的历史数据也还能命中。
			if (allowed[x.Category] || allowed[x.Equipment]) && (input.Focus == "全身" || input.Focus == x.Muscle) && seconds >= 240 {
				day.Items = append(day.Items, Item{Name: x.Name, Sets: 2, Reps: 10, Rest: 60})
				seconds -= 240
			}
		}
		if len(day.Items) == 0 {
			fail(c, 422, "当前器材与部位没有匹配动作，请调整条件")
			return
		}
		plan.Days = append(plan.Days, day)
	}
	c.JSON(200, plan)
}
