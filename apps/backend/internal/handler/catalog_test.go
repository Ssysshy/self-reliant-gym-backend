package handler

import "testing"

// 翻页参数是外部输入，写错不许把请求打成 500，也不许被超大 pageSize 绕过。
func TestParsePage(t *testing.T) {
	cases := []struct {
		no, size             string
		wantNo, wantPageSize int
	}{
		{"", "", 1, 20},        // 缺省：第 1 页、每页 20
		{"3", "50", 3, 50},     // 正常
		{"0", "0", 1, 20},      // 0 视为没给，不能返回空页
		{"-2", "-5", 1, 20},    // 负数归到默认
		{"abc", "xyz", 1, 20},  // 非数字退默认
		{"1", "99999", 1, 200}, // 超上限截到 200
		{"2", "", 2, 20},       // 只给页码，每页走默认 20
	}
	for _, c := range cases {
		no, size := parsePage(c.no, c.size)
		if no != c.wantNo || size != c.wantPageSize {
			t.Fatalf("parsePage(%q,%q) = (%d,%d)，期望 (%d,%d)", c.no, c.size, no, size, c.wantNo, c.wantPageSize)
		}
	}
}
