package handler

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"strconv"
	"strings"
	"time"
)

var conflict = errors.New("数据已更新，请刷新")

// 档位（会员套餐）的两种业务拒绝：天数重复、参数不合法。
// 校验发生在事务里（要读当前行再合并参数），不能直接写响应，所以包成 error 交给 result 翻译。
var dupTier = errors.New("该天数已有档位，换一个天数")

type invalidArg string

func (e invalidArg) Error() string { return string(e) }

func invalid(msg string) error { return invalidArg(msg) }

// 计划保存份数上限：免费 1 份、会员 6 份。上限只拦「新建」，改已有计划不计数，
// 所以会员到期后手上超过 1 份也还能继续改和删，只是暂时不能再加新的。
const (
	FreePlanLimit   = 1
	MemberPlanLimit = 6
)

var (
	freeLimit   = errors.New("免费版只能保存 1 份计划，开通会员可保存 6 份")
	memberLimit = errors.New("会员最多保存 6 份计划，删掉一份再新建")
)

// planLimit 按会员状态算当前能保存的份数，读 /me 与新建计划都以它为准。
func planLimit(u User) int {
	if u.Expires.After(time.Now()) {
		return MemberPlanLimit
	}
	return FreePlanLimit
}

// seed 首次启动写入示范动作与模板。ID 1-8 被下面的示范模板引用，顺序不要动；
// 新动作按部位分组追加在后面，重复执行时靠唯一索引与主键跳过。
func seed() {
	e := db.Transaction(func(tx *gorm.DB) error {
		rows := []Exercise{{ID: 1, Name: "坐姿推胸", Muscle: "胸", SubMuscle: "中胸", Category: "器械", Equipment: "推胸机", Guide: "背部贴垫，向前推起后缓慢还原。", Setup: "调节座椅使把手与胸部同高。"}, {ID: 2, Name: "坐姿划船", Muscle: "背", SubMuscle: "中背", Category: "器械", Equipment: "划船机", Guide: "肩部放松，将把手拉向腹部后还原。", Setup: "脚踩稳踏板，选择可控制的重量。"}, {ID: 3, Name: "腿举", Muscle: "腿", SubMuscle: "股四头", Category: "器械", Equipment: "腿举机", Guide: "保持背部贴垫，屈膝后平稳蹬起。", Setup: "确认安全限位，双脚平放踏板。"}}
		rows = append(rows,
			Exercise{ID: 4, Name: "杯式深蹲", Muscle: "腿", SubMuscle: "股四头", Category: "哑铃", Equipment: "哑铃", Guide: "将哑铃抱在胸前，下蹲后站起。", Setup: "选择能平稳控制的哑铃，双脚站稳。"},
			Exercise{ID: 5, Name: "高位下拉", Muscle: "背", SubMuscle: "背阔肌", Category: "器械", Equipment: "高位下拉机", Guide: "将横杆拉向上胸，缓慢回到起点。", Setup: "调整压腿垫，脚掌平放。"},
			Exercise{ID: 6, Name: "器械推肩", Muscle: "肩", SubMuscle: "前束", Category: "器械", Equipment: "推肩机", Guide: "沿器械轨迹向上推起，缓慢还原。", Setup: "调节座椅，使把手接近肩部高度。"},
			Exercise{ID: 7, Name: "坐姿腿弯举", Muscle: "腿", SubMuscle: "腘绳肌", Category: "器械", Equipment: "腿弯举机", Guide: "屈膝下压滚垫，缓慢还原。", Setup: "调整靠背，使膝关节对齐器械转轴。"},
			Exercise{ID: 8, Name: "臀桥", Muscle: "臀部", SubMuscle: "", Category: "徒手", Equipment: "训练垫", Guide: "屈膝仰卧，抬髋后缓慢放下。", Setup: "平铺训练垫，脚掌踩稳。"})
		// 胸
		rows = append(rows,
			Exercise{ID: 9, Name: "器械上斜卧推（挂片器械）", Muscle: "胸", SubMuscle: "上胸", Category: "器械", Equipment: "上斜卧推机", Guide: "贴紧靠背向上推起，肘部不要锁死，缓慢下放到胸口上沿。", Setup: "调节座椅高度，使把手略低于锁骨。"},
			Exercise{ID: 10, Name: "鹦鹉螺旋机", Muscle: "胸", SubMuscle: "下胸", Category: "器械", Equipment: "鹦鹉螺旋机", Guide: "双臂沿弧线向下向中间夹合，回收时控制速度。", Setup: "按身高调节座椅，前臂贴稳护垫。"},
			Exercise{ID: 11, Name: "肘撑式蝴蝶机夹胸", Muscle: "胸", SubMuscle: "中胸", Category: "器械", Equipment: "蝴蝶机", Guide: "肘部撑住护垫向内夹合，停顿一秒再展开。", Setup: "调节臂垫开度，使前臂水平。"},
			Exercise{ID: 12, Name: "坐姿蝴蝶机夹胸", Muscle: "胸", SubMuscle: "中胸", Category: "器械", Equipment: "蝴蝶机", Guide: "双手握把向内夹合到胸前，还原时不要让配重落下。", Setup: "调节座椅，使握把与肩同高。"},
			Exercise{ID: 13, Name: "器械平板卧推（挂片器械）", Muscle: "胸", SubMuscle: "中胸", Category: "器械", Equipment: "卧推机", Guide: "沿固定轨迹推起，肩胛收紧贴垫。", Setup: "确认挂片左右对称、卡扣锁紧。"},
			Exercise{ID: 14, Name: "杠铃平板卧推", Muscle: "胸", SubMuscle: "中胸", Category: "杠铃", Equipment: "杠铃", Guide: "落到胸部中段后推起，全程肩胛收紧。", Setup: "确认深蹲架挂钩高度，先空杆热身。"},
			Exercise{ID: 15, Name: "上斜哑铃卧推", Muscle: "胸", SubMuscle: "上胸", Category: "哑铃", Equipment: "哑铃", Guide: "哑铃沿胸部两侧上方推起，肘部约 45 度。", Setup: "把椅背调到 30 度左右，哑铃先放在大腿上再躺下。"},
			Exercise{ID: 16, Name: "双杠臂屈伸", Muscle: "胸", SubMuscle: "下胸", Category: "徒手", Equipment: "双杠", Guide: "身体略前倾，屈肘下放后撑起。", Setup: "检查双杠间距与握持稳固。"},
			Exercise{ID: 17, Name: "绳索夹胸", Muscle: "胸", SubMuscle: "中胸", Category: "绳索", Equipment: "龙门架", Guide: "双臂沿弧线在胸前夹合，回收时保持肌肉张力。", Setup: "滑轮调到略高于肩，取中等配重。"},
			Exercise{ID: 18, Name: "史密斯上斜推胸", Muscle: "胸", SubMuscle: "上胸", Category: "史密斯", Equipment: "史密斯机", Guide: "沿轨道推起，下放至锁骨下方。", Setup: "把凳背调到 30 度，轨道对齐胸部上沿。"})
		// 背
		rows = append(rows,
			Exercise{ID: 19, Name: "杠铃硬拉", Muscle: "背", SubMuscle: "下背", Category: "杠铃", Equipment: "杠铃", Guide: "屈髋屈膝下放，收背起身，杠铃贴着腿走。", Setup: "杠铃位于脚掌中段上方，双脚与髋同宽。"},
			Exercise{ID: 20, Name: "单臂哑铃划船", Muscle: "背", SubMuscle: "背阔肌", Category: "哑铃", Equipment: "哑铃", Guide: "把哑铃拉向髋部，肩胛后收，下放时拉长背部。", Setup: "一手一膝撑在训练凳上，背部保持水平。"},
			Exercise{ID: 21, Name: "引体向上", Muscle: "背", SubMuscle: "背阔肌", Category: "徒手", Equipment: "单杠", Guide: "先沉肩再拉起，下巴过杠后缓慢下放。", Setup: "握距略宽于肩，检查单杠是否稳固。"},
			Exercise{ID: 22, Name: "坐姿绳索划船", Muscle: "背", SubMuscle: "中背", Category: "绳索", Equipment: "龙门架", Guide: "把握把拉到腹部，胸口打开，不要用腰后仰借力。", Setup: "滑轮调到与腹部同高，双脚踩稳踏板。"},
			Exercise{ID: 23, Name: "史密斯划船", Muscle: "背", SubMuscle: "中背", Category: "史密斯", Equipment: "史密斯机", Guide: "身体前倾固定角度，把杠铃拉向腹部。", Setup: "杠铃放在膝盖高度，双脚踩在杠杆下方。"})
		// 肩
		rows = append(rows,
			Exercise{ID: 24, Name: "哑铃侧平举", Muscle: "肩", SubMuscle: "中束", Category: "哑铃", Equipment: "哑铃", Guide: "手臂微屈向侧抬起至肩高，慢慢放下。", Setup: "选轻一点的哑铃，站立时核心收紧。"},
			Exercise{ID: 25, Name: "俯身哑铃飞鸟", Muscle: "肩", SubMuscle: "后束", Category: "哑铃", Equipment: "哑铃", Guide: "上身前倾，双臂向两侧打开到与背齐平。", Setup: "屈髋前倾约 45 度，背部挺直。"},
			Exercise{ID: 26, Name: "杠铃颈前推举", Muscle: "肩", SubMuscle: "前束", Category: "杠铃", Equipment: "杠铃", Guide: "把杠铃从锁骨位置推起，收腹避免塌腰。", Setup: "站距与髋同宽，握距略宽于肩。"},
			Exercise{ID: 27, Name: "绳索面拉", Muscle: "肩", SubMuscle: "后束", Category: "绳索", Equipment: "龙门架", Guide: "把绳索拉向面部，手肘向外打开。", Setup: "滑轮调到面部高度，用绳索把手。"})
		// 手臂
		rows = append(rows,
			Exercise{ID: 28, Name: "杠铃弯举", Muscle: "二头", SubMuscle: "", Category: "杠铃", Equipment: "杠铃", Guide: "上臂固定，屈肘把杠铃举起后缓慢放下。", Setup: "站距与髋同宽，握距与肩同宽。"},
			Exercise{ID: 29, Name: "哑铃交替弯举", Muscle: "二头", SubMuscle: "", Category: "哑铃", Equipment: "哑铃", Guide: "两侧交替屈肘，上臂始终贴着身体。", Setup: "双手各持一只哑铃，掌心朝前。"},
			Exercise{ID: 30, Name: "牧师凳弯举", Muscle: "二头", SubMuscle: "", Category: "器械", Equipment: "牧师凳", Guide: "上臂贴住斜垫，屈肘举起后控制下放。", Setup: "调节座椅，使腋下贴住斜垫上沿。"},
			Exercise{ID: 31, Name: "绳索弯举", Muscle: "二头", SubMuscle: "", Category: "绳索", Equipment: "龙门架", Guide: "低位绳索屈肘弯举，顶端收紧一秒。", Setup: "滑轮调到最低位，用直杆把手。"},
			Exercise{ID: 32, Name: "助力臂屈伸", Muscle: "三头", SubMuscle: "", Category: "器械", Equipment: "助力臂屈伸机", Guide: "借助助力向下压，回程时控制速度。", Setup: "助力配重越大越省力，先从小助力开始。"},
			Exercise{ID: 33, Name: "绳索下压", Muscle: "三头", SubMuscle: "", Category: "绳索", Equipment: "龙门架", Guide: "上臂夹紧，向下压到手臂伸直。", Setup: "滑轮调到高位，握住绳索两端。"},
			Exercise{ID: 34, Name: "史密斯窄距卧推", Muscle: "三头", SubMuscle: "", Category: "史密斯", Equipment: "史密斯机", Guide: "握距与肩同宽，下放到胸口后推起。", Setup: "卧推凳放在杠杆正中，调好安全限位。"},
			Exercise{ID: 35, Name: "哑铃颈后臂屈伸", Muscle: "三头", SubMuscle: "", Category: "哑铃", Equipment: "哑铃", Guide: "上臂贴近耳侧，屈肘下放后伸直。", Setup: "单手持哑铃举过头顶，另一手扶住上臂。"})
		// 小臂
		rows = append(rows,
			Exercise{ID: 36, Name: "杠铃腕弯举", Muscle: "小臂", SubMuscle: "", Category: "杠铃", Equipment: "杠铃", Guide: "前臂贴住大腿，只用手腕卷起杠铃。", Setup: "坐姿，前臂平放大腿，掌心向上握杠。"},
			Exercise{ID: 37, Name: "弹力带腕屈伸", Muscle: "小臂", SubMuscle: "", Category: "弹力带", Equipment: "弹力带", Guide: "手腕对抗弹力带屈伸，动作幅度小但要控制。", Setup: "把弹力带踩在脚下，另一头握在手心。"})
		// 腿
		rows = append(rows,
			Exercise{ID: 38, Name: "杠铃深蹲", Muscle: "腿", SubMuscle: "股四头", Category: "杠铃", Equipment: "杠铃", Guide: "屈髋屈膝下蹲到大腿接近水平，再蹬地起身。", Setup: "杠铃放在斜方肌上，先调好深蹲架高度。"},
			Exercise{ID: 39, Name: "腿屈伸", Muscle: "腿", SubMuscle: "股四头", Category: "器械", Equipment: "腿屈伸机", Guide: "伸膝抬起滚垫，顶端停顿后缓慢放下。", Setup: "调节靠背，使膝盖对齐转轴。"},
			Exercise{ID: 40, Name: "史密斯深蹲", Muscle: "腿", SubMuscle: "股四头", Category: "史密斯", Equipment: "史密斯机", Guide: "沿固定轨道下蹲，双脚踩实再起身。", Setup: "双脚前移半步，确认安全挂钩已打开。"})
		// 小腿
		rows = append(rows,
			Exercise{ID: 41, Name: "站姿提踵", Muscle: "小腿", SubMuscle: "", Category: "器械", Equipment: "提踵机", Guide: "踮起脚尖到最高点，停顿后缓慢下落到拉伸位。", Setup: "肩垫调到肩部，前脚掌踩住踏板前沿。"},
			Exercise{ID: 42, Name: "坐姿提踵", Muscle: "小腿", SubMuscle: "", Category: "器械", Equipment: "坐姿提踵机", Guide: "坐姿屈膝状态下踮脚，动作全程不弹震。", Setup: "把护垫压在大腿上方，前脚掌踩稳。"})
		// 臀 / 腹 / 斜方 / 有氧
		rows = append(rows,
			Exercise{ID: 43, Name: "髋外展", Muscle: "臀部", SubMuscle: "", Category: "器械", Equipment: "髋外展机", Guide: "坐姿把双腿向外打开，回收时不要放开配重。", Setup: "调节挡板，使大腿外侧贴住护垫。"},
			Exercise{ID: 44, Name: "弹力带侧步", Muscle: "臀部", SubMuscle: "", Category: "弹力带", Equipment: "弹力带", Guide: "半蹲姿态向侧跨步，全程保持弹力带张力。", Setup: "把环形弹力带套在膝盖上方。"},
			Exercise{ID: 45, Name: "卷腹", Muscle: "腹部", SubMuscle: "", Category: "徒手", Equipment: "训练垫", Guide: "用腹部发力卷起上身，腰部不要离地。", Setup: "仰卧屈膝，双手轻放耳侧。"},
			Exercise{ID: 46, Name: "悬垂举腿", Muscle: "腹部", SubMuscle: "", Category: "徒手", Equipment: "单杠", Guide: "悬垂时收腹举腿，避免摆动借力。", Setup: "双手正握单杠，先确认抓握稳固。"},
			Exercise{ID: 47, Name: "绳索卷腹", Muscle: "腹部", SubMuscle: "", Category: "绳索", Equipment: "龙门架", Guide: "跪姿用腹部把绳索拉下，髋部保持不动。", Setup: "滑轮调到高位，用绳索把手贴在头侧。"},
			Exercise{ID: 48, Name: "杠铃耸肩", Muscle: "斜方肌", SubMuscle: "", Category: "杠铃", Equipment: "杠铃", Guide: "肩部垂直向上耸起，不要绕圈。", Setup: "双手正握杠铃置于身前，站直。"},
			Exercise{ID: 49, Name: "哑铃耸肩", Muscle: "斜方肌", SubMuscle: "", Category: "哑铃", Equipment: "哑铃", Guide: "双臂自然下垂，向上耸肩后缓慢放下。", Setup: "双手各持哑铃，掌心相对。"},
			Exercise{ID: 50, Name: "跑步机快走", Muscle: "有氧", SubMuscle: "", Category: "器械", Equipment: "跑步机", Guide: "保持能说话但略喘的强度，全程匀速。", Setup: "先慢速热身，坡度从 0 开始。"},
			Exercise{ID: 51, Name: "划船机间歇", Muscle: "有氧", SubMuscle: "", Category: "器械", Equipment: "划船机", Guide: "腿、背、手依次发力，回程顺序相反。", Setup: "调节脚踏绑带，确认阻力档位。"},
			Exercise{ID: 52, Name: "动感单车冲刺", Muscle: "有氧", SubMuscle: "", Category: "器械", Equipment: "动感单车", Guide: "冲刺段提高踏频，恢复段降低阻力。", Setup: "调好座椅高度，膝盖微屈即可。"})
		// 官方模板用到的动作（三/四分化里的自由重量与单侧动作），基础库里没有，一起补上。
		// ID 从 54 起：早期的存量数据里 53 已被占用，这里往后排，避免和已有行撞车。
		rows = append(rows,
			Exercise{ID: 54, Name: "Y字侧平举", Muscle: "肩", SubMuscle: "中束", Category: "哑铃", Equipment: "哑铃", Guide: "手肘微屈并外旋，沿Y字斜上方抬至与肩平，顶端停一秒，靠中束发力不耸肩。", Setup: "选轻哑铃，站姿，双臂自然下垂。"},
			Exercise{ID: 55, Name: "单臂钢线下拉", Muscle: "背", SubMuscle: "背阔肌", Category: "绳索", Equipment: "龙门架", Guide: "单手正握把手，肘部贴身下压至腰侧，回放时充分拉伸背阔肌下缘。", Setup: "滑轮调到高位，侧身站位，另一手扶机架。"},
			Exercise{ID: 56, Name: "对握高位下拉", Muscle: "背", SubMuscle: "背阔肌", Category: "器械", Equipment: "高位下拉机", Guide: "掌心相对握把，肩胛先下沉再拉，把横杆拉至上胸，不后仰借力。", Setup: "装对握把手，调腿垫固定大腿。"},
			Exercise{ID: 57, Name: "单臂器械划船", Muscle: "背", SubMuscle: "中背", Category: "器械", Equipment: "划船机", Guide: "单臂拉把手至腹侧，肩胛后缩带动，躯干保持中立不旋转。", Setup: "坐稳压住胸垫，单侧手握住把手。"},
			Exercise{ID: 58, Name: "坐姿开肘划船", Muscle: "背", SubMuscle: "中背", Category: "绳索", Equipment: "龙门架", Guide: "肘部向两侧打开至与肩同高，靠肩胛后缩发力，不靠躯干摆动。", Setup: "低位绳索配双头绳，坐姿双脚蹬实。"},
			Exercise{ID: 59, Name: "单腿硬拉", Muscle: "腿", SubMuscle: "腘绳肌", Category: "哑铃", Equipment: "哑铃", Guide: "髋部后移、上身前倾下放至小腿中段，全程骨盆稳定，靠臀腿拉回。", Setup: "单腿站立，另一腿后伸，双手持一对哑铃。"},
			Exercise{ID: 60, Name: "保加利亚分腿蹲", Muscle: "腿", SubMuscle: "股四头", Category: "哑铃", Equipment: "哑铃", Guide: "后脚搭凳，前腿屈膝下蹲至大腿接近水平，膝盖对准脚尖方向。", Setup: "训练凳置于身后约一步，双手持哑铃。"},
			Exercise{ID: 61, Name: "杠铃颈前深蹲", Muscle: "腿", SubMuscle: "股四头", Category: "杠铃", Equipment: "杠铃", Guide: "杠铃架在锁骨前侧，躯干保持直立下蹲，肘部高抬防止前倾。", Setup: "深蹲架调到肩高，用颈前架杠姿势起杠。"},
			Exercise{ID: 62, Name: "罗马尼亚硬拉", Muscle: "腿", SubMuscle: "腘绳肌", Category: "杠铃", Equipment: "杠铃", Guide: "膝微屈固定，杠铃贴腿下滑至小腿中段，靠臀部后移拉回。", Setup: "站距与髋同宽，正握杠铃置于大腿前。"},
			Exercise{ID: 63, Name: "山羊挺身", Muscle: "背", SubMuscle: "下背", Category: "器械", Equipment: "罗马椅", Guide: "上身前屈下放，靠臀与竖脊肌发力回到与腿成一条线，不超伸。", Setup: "罗马椅调至髋部可自由折叠，脚踝固定。"},
			Exercise{ID: 64, Name: "史密斯卧推", Muscle: "胸", SubMuscle: "中胸", Category: "史密斯", Equipment: "史密斯机", Guide: "落到胸部中段后推起，轨迹固定，全程肩胛收紧。", Setup: "卧推凳置于史密斯机中间，调好挂钩高度。"},
			Exercise{ID: 65, Name: "器械下斜卧推", Muscle: "胸", SubMuscle: "下胸", Category: "器械", Equipment: "下斜卧推机", Guide: "握把推到肘接近伸直，靠胸肌下缘发力，回放控制到舒适位。", Setup: "调整座椅使握把与下胸齐平。"},
			Exercise{ID: 66, Name: "站姿直臂下压", Muscle: "背", SubMuscle: "背阔肌", Category: "绳索", Equipment: "龙门架", Guide: "手臂伸直下压至大腿前，靠背阔肌带动，躯干不前后晃。", Setup: "高位绳索配直杆，站距与髋同宽。"},
			Exercise{ID: 67, Name: "坐姿反向飞鸟", Muscle: "肩", SubMuscle: "后束", Category: "器械", Equipment: "反向飞鸟机", Guide: "手肘微抬向两侧打开，意念是向外延伸，回放不触底保持张力。", Setup: "调整座椅使把手与肩齐平，胸口贴紧靠垫。"},
			Exercise{ID: 68, Name: "史密斯推肩", Muscle: "肩", SubMuscle: "前束", Category: "史密斯", Equipment: "史密斯机", Guide: "杠铃推至头顶上方，肘部略前于身体，下放至下巴高度。", Setup: "调好座椅高度，坐下后杠铃在下巴位置。"},
			Exercise{ID: 69, Name: "哑铃前平举", Muscle: "肩", SubMuscle: "前束", Category: "哑铃", Equipment: "哑铃", Guide: "对握举至与肩同高，下放不甩不借力，保持挺胸。", Setup: "站姿双手对握哑铃置于体前。"},
			Exercise{ID: 70, Name: "上斜杠铃臂屈伸", Muscle: "三头", SubMuscle: "", Category: "杠铃", Equipment: "杠铃", Guide: "躺上斜凳，杠铃下放至额头上方，肘部固定不外张，靠三头伸直。", Setup: "上斜凳调至45度，窄握杠铃举过头顶。"},
			Exercise{ID: 71, Name: "蹲姿二头弯举", Muscle: "二头", SubMuscle: "", Category: "哑铃", Equipment: "哑铃", Guide: "蹲姿固定躯干，肘部贴大腿内侧屈肘弯举，顶端挤压一秒。", Setup: "深蹲位，一手扶膝，另一手持哑铃。"},
			Exercise{ID: 72, Name: "坐姿内收", Muscle: "臀部", SubMuscle: "", Category: "器械", Equipment: "内收机", Guide: "双腿由外向内夹紧，顶端停顿一秒，控制回放不猛松。", Setup: "坐稳内收机，调好挡板起始角度。"},
			Exercise{ID: 73, Name: "哑铃弓步蹲", Muscle: "腿", SubMuscle: "股四头", Category: "哑铃", Equipment: "哑铃", Guide: "前腿屈膝下蹲至大腿接近水平，后膝接近地面，躯干保持直立。", Setup: "双手持哑铃垂于体侧，站距与髋同宽。"},
			Exercise{ID: 74, Name: "杠铃提踵", Muscle: "小腿", SubMuscle: "", Category: "杠铃", Equipment: "杠铃", Guide: "前脚掌踩实，提踵到最高点停顿一秒，下放到小腿充分拉长。", Setup: "杠铃置于斜方肌上，前脚掌踩在杠片或台阶边缘。"})
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error; e != nil {
			return e
		}
		// 官方模板固定 ID 11/12，和已经上线的存量数据对齐（早期的 1~3 已下线，不再种回来，
		// 否则老库重启一次就会把删掉的模板重新插进去）。
		// 详情页文案放在 Meta.Blocks 里按顺序渲染；训练安排由 Days 自动生成，不另写一份。
		at := func(w int, focus string, items []Item) Day {
			// 同一份动作清单会被周内两个训练日复用，拷一份，免得两边共享底层数组
			return Day{Weekday: w, Focus: focus, Items: append([]Item(nil), items...)}
		}
		push := []Item{
			{"杠铃平板卧推", 4, 10, 90},
			{"上斜哑铃卧推", 4, 12, 90},
			{"双杠臂屈伸", 4, 12, 90},
			{"哑铃颈后臂屈伸", 4, 15, 60},
			{"Y字侧平举", 3, 20, 60},
		}
		pull := []Item{
			{"单臂钢线下拉", 4, 12, 90},
			{"对握高位下拉", 4, 10, 90},
			{"单臂器械划船", 4, 10, 90},
			{"坐姿开肘划船", 4, 12, 90},
			{"绳索弯举", 3, 12, 60},
		}
		legs := []Item{
			{"单腿硬拉", 4, 12, 90},
			{"保加利亚分腿蹲", 4, 10, 90},
			{"杠铃颈前深蹲", 3, 15, 90},
			{"罗马尼亚硬拉", 4, 12, 90},
			{"山羊挺身", 3, 8, 60},
		}
		chest := []Item{
			{"史密斯卧推", 4, 12, 90},
			{"器械上斜卧推（挂片器械）", 4, 12, 90},
			{"坐姿蝴蝶机夹胸", 4, 12, 60},
			{"器械下斜卧推", 4, 12, 90},
			{"杠铃颈前推举", 4, 12, 90},
			{"哑铃颈后臂屈伸", 4, 12, 60},
		}
		back := []Item{
			{"站姿直臂下压", 4, 12, 60},
			{"坐姿划船", 4, 12, 90},
			{"高位下拉", 4, 12, 90},
			{"引体向上", 4, 12, 90},
			{"杠铃弯举", 4, 12, 60},
		}
		shoulder := []Item{
			{"坐姿反向飞鸟", 4, 12, 60},
			{"史密斯推肩", 4, 12, 90},
			{"哑铃前平举", 4, 12, 60},
			{"Y字侧平举", 4, 12, 60},
			{"上斜杠铃臂屈伸", 4, 12, 60},
			{"蹲姿二头弯举", 4, 12, 60},
		}
		lower := []Item{
			{"坐姿内收", 4, 12, 60},
			{"哑铃弓步蹲", 4, 12, 90},
			{"单腿硬拉", 4, 12, 90},
			{"杠铃提踵", 4, 12, 60},
			{"悬垂举腿", 4, 12, 60},
			{"绳索卷腹", 4, 12, 60},
			{"卷腹", 4, 12, 60},
		}
		for _, p := range []Plan{
			{
				ID: 11, Name: "谭成义&凯圣王三分化", Published: true, Revision: 1,
				Meta: &PlanMeta{
					Cover:   "/assets/plans/tan-3split.jpg",
					Level:   "中级",
					Minutes: 35,
					Blocks: []Block{
						{Title: "计划介绍", Text: "凯圣王 × 谭成义联名的推拉腿三分化：按发力模式把身体拆成推、拉、腿三个单元，练 3 天休 1 天，一周走完两轮循环，让每个部位每周被刺激两次。"},
						{Title: "计划结构", Lines: []string{
							"① 推日 · 胸 / 三角肌中束 / 肱三头（推类核心）",
							"② 拉日 · 背 / 三角肌后束 / 肱二头（拉类核心）",
							"③ 腿日 · 臀 / 股四 / 腘绳（后侧链主导）",
						}},
						{Title: "核心理念", Text: "以杠铃卧推这类自由重量筛查并开发身体稳定性与协调性，而不是只靠器械堆容量。推日不再单独加推肩——卧推、上斜与双杠已经把前束喂饱，只补一个 Y 字侧平举专攻中束。"},
						{Title: "主项线性渐进", Lines: []string{
							"卧推 15 → 12 → 10 → 8 → 6 次，正式组 RPE8，留约 2 次余量",
							"提高强度时先减组再加组：2 组 × 6 → 3 组 × 6 → 4 组 × 6",
							"复合动作不力竭，孤立动作可以做到力竭",
							"每次训练只动一个变量：要么加重量，要么加组数",
						}},
					},
				},
				Days: []Day{
					at(1, "推日 · 胸/肩中束/三头", push),
					at(2, "拉日 · 背/肩后束/二头", pull),
					at(3, "腿日 · 臀/后侧链/核心", legs),
					at(5, "推日 · 胸/肩中束/三头", push),
					at(6, "拉日 · 背/肩后束/二头", pull),
					at(7, "腿日 · 臀/后侧链/核心", legs),
				},
			},
			{
				ID: 12, Name: "谭成义四分化", Published: true, Revision: 1,
				Meta: &PlanMeta{
					Cover:   "/assets/plans/tan-4split.jpg",
					Level:   "入门",
					Minutes: 41,
					Blocks: []Block{
						{Title: "计划介绍", Text: "谭成义的新手友好型四分化：一周练 4 天，按胸加三头、背加二头、肩加手臂、臀腿加腹拆成四份，其余时间留给恢复，节奏更适合上班族。"},
						{Title: "计划结构", Lines: []string{
							"① 胸 + 肱三头",
							"② 背 + 肱二头",
							"③ 肩 + 手臂",
							"④ 臀腿 + 腹",
						}},
						{Title: "核心理念", Text: "先把动作质量、力线与肩胛控制做对，再谈重量。每个动作统一 4 组 × 12 次，不堆花哨动作，把基础动作重复到标准为止。"},
						{Title: "执行要点", Lines: []string{
							"大重量复合动作留余力，动作一变形就停",
							"小重量孤立动作可以练到接近力竭",
							"同一个部位之间至少留 48 小时恢复",
							"先把基础动作练两个月，别急着换计划",
						}},
					},
				},
				Days: []Day{
					at(1, "胸 + 三头", chest),
					at(2, "背 + 二头", back),
					at(4, "肩 + 手臂", shoulder),
					at(5, "臀腿 + 腹", lower),
				},
			},
		} {
			if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&p).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if e != nil {
		log.Fatal(e)
	}
}
func valid(p Plan) bool {
	if strings.TrimSpace(p.Name) == "" || len(p.Days) == 0 || len(p.Days) > 7 {
		return false
	}
	seen := map[int]bool{}
	for _, d := range p.Days {
		if seen[d.Weekday] || d.Weekday < 1 || d.Weekday > 7 || d.Focus == "" || len(d.Items) == 0 || len(d.Items) > 30 {
			return false
		}
		seen[d.Weekday] = true
		for _, i := range d.Items {
			if i.Name == "" || i.Sets < 1 || i.Sets > 30 || i.Reps < 1 || i.Reps > 200 || (i.Rest != 30 && i.Rest != 60 && i.Rest != 90) {
				return false
			}
		}
	}
	return true
}
func locked(tx *gorm.DB, id uint) (User, error) {
	var u User
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, id).Error
	return u, e
}
func result(c *gin.Context, v any, e error) {
	if errors.Is(e, conflict) {
		fail(c, 409, e.Error())
		return
	}
	// 计划份数到顶属于「说得清的业务拒绝」：403 + 直出文案，前端 report 会原样弹出来
	if errors.Is(e, freeLimit) || errors.Is(e, memberLimit) {
		fail(c, 403, e.Error())
		return
	}
	// 档位编辑：参数不合法是 400，天数撞车是 409
	var bad invalidArg
	if errors.As(e, &bad) {
		fail(c, 400, bad.Error())
		return
	}
	if errors.Is(e, dupTier) {
		fail(c, 409, e.Error())
		return
	}
	save(c, v, e)
}
func registerPlans(g *gin.RouterGroup) {
	g.GET("/templates", func(c *gin.Context) {
		var v []Plan
		e := db.Where("owner=0 AND published=true").Find(&v).Error
		save(c, v, e)
	})
	g.GET("/plans", func(c *gin.Context) {
		var v []Plan
		e := db.Where("owner=?", uid(c)).Order("id desc").Find(&v).Error
		save(c, v, e)
	})
	g.POST("/plans", func(c *gin.Context) {
		var in struct {
			Operation                  string
			Template, Revision, Active uint
			// ID 非 0 表示「改已有计划」：原地更新那一条，不再另存一份
			ID     uint
			Custom Plan
		}
		if c.ShouldBindJSON(&in) != nil {
			fail(c, 400, "方案无效")
			return
		}
		p := Plan{Name: in.Custom.Name, Days: in.Custom.Days}
		if in.Template == 0 && !valid(p) {
			fail(c, 400, "请检查训练安排")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			u, e := locked(tx, uid(c))
			if e != nil {
				return e
			}
			if len(in.Operation) > 72 || len(in.Operation) < 8 {
				return conflict
			}
			// 改已有计划：只覆盖名称和训练日，owner/operation_id 保持不动。
			// 更新本身幂等，不用像新建那样按 Operation 查重放记录（那条重放要在
			// Active 校验之前，否则重试会被误判成 409）。
			if in.ID != 0 {
				if u.Active != in.Active {
					return conflict
				}
				var cur Plan
				if e := tx.Where("owner=? AND id=?", u.ID, in.ID).First(&cur).Error; e != nil {
					if errors.Is(e, gorm.ErrRecordNotFound) {
						return conflict
					}
					return e
				}
				cur.Name, cur.Days, cur.Published = p.Name, p.Days, false
				if e := tx.Save(&cur).Error; e != nil {
					return e
				}
				p = cur
				return tx.Model(&u).Update("active", cur.ID).Error
			}
			var previous Plan
			err := tx.Where("owner=? AND operation_id=?", u.ID, in.Operation).First(&previous).Error
			if err == nil {
				p = previous
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if u.Active != in.Active {
				return conflict
			}
			// 份数上限：必须放在 Operation 重放查询之后 —— 同一次提交重试时应当返回已建好的那份，
			// 而不是被自己的上限挡住。这里只数 owner=自己的计划，官方模板（owner=0）不计入。
			var count int64
			if e := tx.Model(&Plan{}).Where("owner=?", u.ID).Count(&count).Error; e != nil {
				return e
			}
			if limit := planLimit(u); count >= int64(limit) {
				if limit == FreePlanLimit {
					return freeLimit
				}
				return memberLimit
			}
			if in.Template != 0 {
				if e := tx.Where("owner=0 AND published=true AND revision=?", in.Revision).First(&p, in.Template).Error; e != nil {
					return e
				}
			}
			p.ID = 0
			p.OperationID = &in.Operation
			p.Owner = u.ID
			p.Published = false
			// 模板的封面与介绍文案不跟着复制：用户拿到的是可改的计划，留着那份推广文案反而会误导
			p.Meta = nil
			if e := tx.Create(&p).Error; e != nil {
				return e
			}
			return tx.Model(&u).Update("active", p.ID).Error
		})
		result(c, p, e)
	})
	// 删整份计划（连同它的训练日一起）。删的是当前计划时，当前位顶到剩下最新的一份；
	// 一份都不剩就置 0，前端据此回到「还没有计划」的引导态。历史训练记录是当时的 Day
	// 快照，不跟着删。返回 Active 让客户端不必再查一次 /me。
	g.DELETE("/plans/:id", func(c *gin.Context) {
		id, _ := strconv.Atoi(c.Param("id"))
		active := uint(0)
		e := db.Transaction(func(tx *gorm.DB) error {
			u, e := locked(tx, uid(c))
			if e != nil {
				return e
			}
			var p Plan
			if e := tx.Where("owner=? AND id=?", u.ID, id).First(&p).Error; e != nil {
				return e
			}
			if e := tx.Delete(&Plan{}, p.ID).Error; e != nil {
				return e
			}
			active = u.Active
			if u.Active != p.ID {
				return nil
			}
			var next Plan
			err := tx.Where("owner=?", u.ID).Order("id desc").First(&next).Error
			if err == nil {
				active = next.ID
			} else if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if e := tx.Model(&u).Update("active", active).Error; e != nil {
				return e
			}
			return nil
		})
		if e != nil {
			save(c, nil, e)
			return
		}
		save(c, gin.H{"Active": active}, nil)
	})
}

// pickDay 从计划里挑一个训练日：weekday 为 0 时取今天或之后最近的一天。
func pickDay(p Plan, weekday int) (Day, bool) {
	today := int(time.Now().In(time.FixedZone("CST", 8*3600)).Weekday())
	best := 8
	var pick Day
	for _, d := range p.Days {
		if weekday != 0 && d.Weekday != weekday {
			continue
		}
		if gap := (d.Weekday - today + 7) % 7; gap < best {
			best = gap
			pick = d
		}
	}
	return pick, best != 8
}

func registerTraining(g *gin.RouterGroup) {
	g.GET("/sessions/:id", func(c *gin.Context) {
		var s Session
		e := db.Where("owner=? AND id=?", uid(c), c.Param("id")).First(&s).Error
		save(c, s, e)
	})
	g.GET("/sessions", func(c *gin.Context) {
		var v []Session
		e := db.Where("owner=?", uid(c)).Order("id desc").Limit(100).Find(&v).Error
		save(c, v, e)
	})
	// 预览：只按计划算出这次要练哪一天，不落库，供训练页在没有本地进度时开新的一场
	g.GET("/sessions/preview", func(c *gin.Context) {
		plan, _ := strconv.Atoi(c.Query("plan"))
		weekday, _ := strconv.Atoi(c.Query("weekday"))
		var p Plan
		if e := db.Where("owner=?", uid(c)).First(&p, plan).Error; e != nil {
			save(c, nil, e)
			return
		}
		day, ok := pickDay(p, weekday)
		if !ok {
			fail(c, 404, "这个计划没有安排训练日")
			return
		}
		c.JSON(200, gin.H{"PlanID": p.ID, "Day": day})
	})
	// 提交一场训练：客户端把整段记录一次传上来，operation 保证重试不会写两条
	g.POST("/sessions", func(c *gin.Context) {
		var in struct {
			Plan      uint
			Weekday   int
			Started   time.Time
			Done      []int
			Weights   []*float64
			Operation string
		}
		if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Operation) == "" || len(in.Done) == 0 {
			fail(c, 400, "记录无效")
			return
		}
		var p Plan
		if e := db.Where("owner=?", uid(c)).First(&p, in.Plan).Error; e != nil {
			save(c, nil, e)
			return
		}
		day, ok := pickDay(p, in.Weekday)
		if !ok {
			fail(c, 400, "这个计划没有安排训练日")
			return
		}
		total := 0
		for _, i := range day.Items {
			total += i.Sets
		}
		if len(in.Done) != total {
			fail(c, 400, "组数和计划不一致")
			return
		}
		for _, n := range in.Done {
			if n < 0 || n > 200 {
				fail(c, 400, "次数须为 1 至 200")
				return
			}
		}
		for _, w := range in.Weights {
			if w != nil && (*w < 0 || *w > 2000) {
				fail(c, 400, "重量无效")
				return
			}
		}
		for len(in.Weights) < len(in.Done) {
			in.Weights = append(in.Weights, nil)
		}
		in.Weights = in.Weights[:len(in.Done)]
		started := in.Started
		if started.IsZero() || started.After(time.Now()) {
			started = time.Now()
		}
		ended := time.Now()
		s := Session{Owner: uid(c), PlanID: p.ID, Day: day, Done: in.Done, Weights: in.Weights, Started: started, Ended: &ended, OperationID: &in.Operation}
		if e := db.Create(&s).Error; e != nil {
			if !strings.Contains(e.Error(), "Duplicate entry") {
				save(c, nil, e)
				return
			}
			if e := db.Where("owner=? AND operation_id=?", uid(c), in.Operation).First(&s).Error; e != nil {
				save(c, nil, e)
				return
			}
		}
		c.JSON(200, s)
	})
}
func registerAdmin(g *gin.RouterGroup) {
	templateEditor(g)
	g.POST("/logout", func(c *gin.Context) {
		if e := db.Delete(&StaffSession{}, "id=?", c.GetString("sid")).Error; e != nil {
			save(c, nil, e)
			return
		}
		c.SetCookie("gym_admin", "", -1, "/api/admin", "", true, true)
		c.JSON(200, true)
	})
	g.GET("/exercises", exerciseList)
	// 部位与器材分类的约定顺序：后台下拉和小程序侧边栏共用一份，避免两边手工对齐。
	g.GET("/categories", categories)
	// 用户反馈：后台按时间倒序看最新 200 条，可按状态、用户编号或内容关键词筛。
	// 用户报上来的是编号（U3M8Q2K），后台旧习惯的纯数字 ID 也照旧能搜。
	g.GET("/feedbacks", func(c *gin.Context) {
		var rows []Feedback
		q := db.Order("id desc").Limit(200)
		if st := strings.TrimSpace(c.Query("status")); st != "" {
			q = q.Where("status=?", st)
		}
		if owner := strings.TrimSpace(c.Query("owner")); owner != "" {
			id, ok := ownerID(owner)
			if !ok {
				// 编号查不到人就当作没这个人：返回空列表，比 400 更贴近「没搜到」
				save(c, []Feedback{}, nil)
				return
			}
			q = q.Where("owner=?", id)
		}
		if kw := strings.TrimSpace(c.Query("q")); kw != "" {
			q = q.Where("content LIKE ?", "%"+kw+"%")
		}
		e := q.Find(&rows).Error
		owners := make([]uint, 0, len(rows))
		for _, r := range rows {
			owners = append(owners, r.Owner)
		}
		codes := codesOf(owners)
		for i := range rows {
			rows[i].OwnerCode = codes[rows[i].Owner]
		}
		save(c, rows, e)
	})
	// 标记处理状态：已处理时记处理人与时间，退回待处理时清空
	g.PUT("/feedbacks/:id", func(c *gin.Context) {
		var in struct {
			Status string
			Note   string
		}
		if c.ShouldBindJSON(&in) != nil || (in.Status != "待处理" && in.Status != "已处理") {
			fail(c, 400, "处理状态无效")
			return
		}
		if len([]rune(in.Note)) > 500 {
			fail(c, 400, "备注最多 500 字")
			return
		}
		var row Feedback
		if e := db.First(&row, "id=?", c.Param("id")).Error; e != nil {
			save(c, nil, e)
			return
		}
		updates := map[string]any{"status": in.Status, "note": strings.TrimSpace(in.Note)}
		if in.Status == "已处理" {
			updates["handled_by"] = c.GetString("actor")
			updates["handled_at"] = time.Now()
		} else {
			updates["handled_by"] = nil
			updates["handled_at"] = nil
		}
		if e := db.Model(&Feedback{}).Where("id=?", row.ID).Updates(updates).Error; e != nil {
			save(c, nil, e)
			return
		}
		// 用新结构体重新读，避免在已有值上扫描 NULL 时保留旧的指针字段
		var fresh Feedback
		save(c, fresh, db.First(&fresh, row.ID).Error)
	})
	g.POST("/exercises", func(c *gin.Context) {
		var x Exercise
		if c.ShouldBindJSON(&x) != nil || x.Name == "" || x.Guide == "" || x.Equipment == "" || x.Setup == "" {
			fail(c, 400, "请填写完整资料")
			return
		}
		if msg := taxonomy(x); msg != "" {
			fail(c, 400, msg)
			return
		}
		if x.ImageURL != "" && !strings.HasPrefix(x.ImageURL, "https://") {
			fail(c, 400, "教学图须使用 HTTPS 地址")
			return
		}
		if x.ID != 0 {
			var old Exercise
			if e := db.First(&old, x.ID).Error; e != nil {
				save(c, nil, e)
				return
			}
			if old.Name != x.Name {
				fail(c, 409, "已建动作名称用于关联训练模板，请新增动作")
				return
			}
		}
		save(c, x, db.Save(&x).Error)
	})
	// 会员列表：支持按用户编号（U3M8Q2K）或纯数字 ID 查人，客服照着用户报的编号就能找到
	g.GET("/members", func(c *gin.Context) {
		var rows []User
		q := db.Order("id desc").Limit(100)
		if owner := strings.TrimSpace(c.Query("id")); owner != "" {
			id, ok := ownerID(owner)
			if !ok {
				save(c, []User{}, nil)
				return
			}
			q = q.Where("id=?", id)
		}
		save(c, rows, q.Find(&rows).Error)
	})
	g.POST("/members/:id/grants", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		var in struct {
			Days     int
			Reason   string
			Previous time.Time
		}
		if c.ShouldBindJSON(&in) != nil || in.Days < 1 || in.Days > 365 || strings.TrimSpace(in.Reason) == "" {
			fail(c, 400, "请输入天数和原因")
			return
		}
		e := db.Transaction(func(tx *gorm.DB) error {
			var u User
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id=?", c.Param("id")).Error; e != nil {
				return e
			}
			if !u.Expires.Equal(in.Previous) {
				return conflict
			}
			base := u.Expires
			if base.Before(time.Now()) {
				base = time.Now()
			}
			if e := tx.Create(&Grant{UserID: u.ID, Days: in.Days, Reason: in.Reason, Operator: "admin"}).Error; e != nil {
				return e
			}
			return tx.Model(&u).Update("expires", base.AddDate(0, 0, in.Days)).Error
		})
		result(c, true, e)
	})
	// 注销账号：把这个人彻底删掉 —— 计划、训练记录、反馈、人工权益流水和账号行一起删。
	// 已支付/已退款的订单不删：交易要留档，但把 owner 置 0 脱敏，从此对不上具体的人；
	// 退款回调也是靠 owner 找用户，所以 refund 里对 owner=0 单独处理。
	// 还有「待支付」订单时直接拒绝：那一刻钱可能刚付出去、回调还没到，删了就对不上账。
	g.DELETE("/members/:id", func(c *gin.Context) {
		if !c.GetBool("admin") {
			fail(c, 403, "需要管理员权限")
			return
		}
		id, e := strconv.ParseUint(c.Param("id"), 10, 32)
		if e != nil {
			fail(c, 400, "账号无效")
			return
		}
		// 要求照着用户编号再输一遍：注销不可恢复，只靠点一次确认太容易误操作
		var in struct{ Code string }
		if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Code) == "" {
			fail(c, 400, "请输入用户编号确认")
			return
		}
		var count struct{ Plans, Sessions, Feedbacks, Grants, Orders int64 }
		e = db.Transaction(func(tx *gorm.DB) error {
			var u User
			if e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, "id=?", id).Error; e != nil {
				return e
			}
			if !strings.EqualFold(strings.TrimSpace(in.Code), u.Code) {
				return invalid("用户编号不匹配，未执行注销")
			}
			var pending int64
			if e := tx.Model(&Order{}).Where("owner=? AND status=?", u.ID, "待支付").Count(&pending).Error; e != nil {
				return e
			}
			if pending > 0 {
				return invalid("该账号还有未完成的订单，等支付结果确认后再注销")
			}
			plans := tx.Where("owner=?", u.ID).Delete(&Plan{})
			if plans.Error != nil {
				return plans.Error
			}
			sessions := tx.Where("owner=?", u.ID).Delete(&Session{})
			if sessions.Error != nil {
				return sessions.Error
			}
			feedbacks := tx.Where("owner=?", u.ID).Delete(&Feedback{})
			if feedbacks.Error != nil {
				return feedbacks.Error
			}
			grants := tx.Where("user_id=?", u.ID).Delete(&Grant{})
			if grants.Error != nil {
				return grants.Error
			}
			count.Plans, count.Sessions = plans.RowsAffected, sessions.RowsAffected
			count.Feedbacks, count.Grants = feedbacks.RowsAffected, grants.RowsAffected
			// 订单脱敏：owner 置 0，其余字段（金额、天数、支付状态、渠道单号）留着对账
			res := tx.Model(&Order{}).Where("owner=?", u.ID).Update("owner", 0)
			if res.Error != nil {
				return res.Error
			}
			count.Orders = res.RowsAffected
			if e := tx.Delete(&User{}, u.ID).Error; e != nil {
				return e
			}
			action := fmt.Sprintf("注销账号 %s：计划 %d、训练记录 %d、反馈 %d、权益流水 %d、订单 %d 笔脱敏保留",
				u.Code, count.Plans, count.Sessions, count.Feedbacks, count.Grants, count.Orders)
			return tx.Create(&Audit{Actor: c.GetString("actor"), Action: action, Target: u.ID}).Error
		})
		result(c, count, e)
	})
}
