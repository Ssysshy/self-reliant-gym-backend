package handler

import (
	"log"
	"strconv"
	"strings"
)

// 用户编号：对外展示的账号标识，形如 U3M8Q2K，落库存在 users.code（见 migrations/007）。
//
// 为什么不直接把自增 ID 给用户：早期用户会看到「3」这种一位数，一眼就看出产品刚上线；
// 口头报给客服时读错一位也查不到人。编号由 ID 派生，同一个用户每次算出来都一样：
//
//	U + 负载 5 位 + 校验 1 位，每 5 bit 一个字符
//	负载 = (id * scramble) mod 2^25，scramble 取奇数，相邻 ID 的编号面目全非
//	字母表去掉 0/1/I/O，念出来不会听混；前缀 U 让客服一眼分清「编号」和「纯数字 ID」
//
// 2^25 够 3300 万个账号；超过以后编号会和更早的号撞车，那时得换一套取值方式。
// 好在编号已经落库，换算法只影响新用户，已经发出去的编号不用动。
const (
	codePrefix   = "U"
	codeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codePayload  = 5
	codeScramble = 0x9E3779B1
	codeMask     = 1<<(codePayload*5) - 1
)

// Code 按用户 ID 生成编号。只在注册、回填和兜底时调用；读接口一律读 users.code。
func Code(id uint) string {
	v := (uint64(id) * codeScramble) & codeMask
	out := make([]byte, codePayload+1)
	// 低位在右：先算校验位，再从后往前填负载
	sum := 0
	for i := codePayload - 1; i >= 0; i-- {
		d := int(v & 31)
		v >>= 5
		out[i] = codeAlphabet[d]
		sum += (i + 1) * d
	}
	out[codePayload] = codeAlphabet[sum%32]
	return codePrefix + string(out)
}

// codeOf 读用户编号，缺了就当场补一次并落库。
// 正常由启动时的 ensureCodes 补齐，这里兜住「迁移之后、回填之前」新建或恢复出来的行，
// 免得页面上出现空编号。
func codeOf(u *User) string {
	if u.Code != "" {
		return u.Code
	}
	u.Code = Code(u.ID)
	if e := db.Model(&User{}).Where("id=?", u.ID).Update("code", u.Code).Error; e != nil {
		log.Printf("用户 %d 补编号失败：%v", u.ID, e)
	}
	return u.Code
}

// ensureCodes 洗存量数据：迁移只加列，老用户在这里按 ID 一次性补齐编号。
// 编号由 ID 派生，重复执行只会算出同样的值，所以每次启动跑一遍最省心
// （没有待补的行时就是一次空查询）。
func ensureCodes() {
	var ids []uint
	if e := db.Model(&User{}).Where("code IS NULL OR code = ''").Order("id").Pluck("id", &ids).Error; e != nil {
		log.Println("读取待补编号的用户失败：", e)
		return
	}
	for _, id := range ids {
		if e := db.Model(&User{}).Where("id=?", id).Update("code", Code(id)).Error; e != nil {
			log.Printf("用户 %d 补编号失败：%v", id, e)
		}
	}
	if len(ids) > 0 {
		log.Printf("已回填用户编号 %d 个", len(ids))
	}
}

// codesOf 批量取编号：后台反馈列表要按 Owner 显示编号，不逐行查库。
func codesOf(ids []uint) map[uint]string {
	out := make(map[uint]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	var rows []User
	if e := db.Model(&User{}).Select("id", "code").Where("id IN ?", ids).Find(&rows).Error; e != nil {
		return out
	}
	for _, u := range rows {
		out[u.ID] = u.Code
	}
	return out
}

// validCode 校验编号的格式和校验位。后台输入框先过这一关：明显输错的编号不必查库，
// 也让校验位真正派上用场（输错一位不会静默查到别人）。
func validCode(s string) bool {
	s = strings.ToUpper(strings.TrimSpace(s))
	if !strings.HasPrefix(s, codePrefix) || len(s) != len(codePrefix)+codePayload+1 {
		return false
	}
	s = s[len(codePrefix):]
	sum := 0
	for i := 0; i < codePayload; i++ {
		d := strings.IndexByte(codeAlphabet, s[i])
		if d < 0 {
			return false
		}
		sum += (i + 1) * d
	}
	return s[codePayload] == codeAlphabet[sum%32]
}

// ownerID 解析后台输入框里的用户标识：纯数字仍然当作老的「用户 ID」用，
// 其余按库里的编号查（编号以 U 开头，不会和纯数字混淆）。
func ownerID(s string) (uint, bool) {
	s = strings.TrimSpace(s)
	if n, e := strconv.ParseUint(s, 10, 64); e == nil {
		return uint(n), true
	}
	if !validCode(s) {
		return 0, false
	}
	var u User
	if e := db.Where("code=?", strings.ToUpper(s)).First(&u).Error; e != nil {
		return 0, false
	}
	return u.ID, true
}
