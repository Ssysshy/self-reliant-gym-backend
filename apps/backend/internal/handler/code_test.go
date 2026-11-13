package handler

import (
	"strings"
	"testing"
)

// 编号格式固定：U + 5 位负载 + 1 位校验，字母表里没有 0/1/I/O。
func TestCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for id := uint(1); id <= 200; id++ {
		s := Code(id)
		if len(s) != 7 || s[0] != 'U' {
			t.Fatalf("id=%d 的编号 %q 不是 U + 6 位", id, s)
		}
		for _, c := range s[1:] {
			if !strings.ContainsRune(codeAlphabet, c) {
				t.Fatalf("id=%d 的编号 %q 含字母表外的字符 %q", id, s, c)
			}
		}
		if seen[s] {
			t.Fatalf("编号 %q 重复", s)
		}
		seen[s] = true
		if !validCode(s) {
			t.Fatalf("id=%d 生成的编号 %q 自己都过不了校验", id, s)
		}
	}
}

// 同一个 ID 每次算出来必须一样，回填才是幂等的。
func TestCodeStable(t *testing.T) {
	if a, b := Code(1234), Code(1234); a != b {
		t.Fatalf("同一 ID 两次生成不一致：%q / %q", a, b)
	}
}

// 相邻两个号的编号要面目全非，否则还是能顺着看出用户量和注册顺序。
func TestCodeOpaqueForEarlyUsers(t *testing.T) {
	prev := ""
	for id := uint(1); id <= 50; id++ {
		s := Code(id)
		if prev != "" {
			diff := 0
			for i := 1; i < len(s); i++ {
				if s[i] != prev[i] {
					diff++
				}
			}
			if diff < 4 {
				t.Fatalf("id=%d：%q 与上一个 %q 只差 %d 位", id, s, prev, diff)
			}
		}
		prev = s
	}
}

// 输入侧的容忍度和拒绝：大小写/空格能认，改校验位、位数不对、字符越界都要判无效。
func TestValidCode(t *testing.T) {
	s := Code(42)
	for _, in := range []string{s, " " + strings.ToLower(s) + " "} {
		if !validCode(in) {
			t.Fatalf("%q 应判有效", in)
		}
	}
	bad := []byte(s)
	bad[6] = 'Z'
	if bad[6] == s[6] {
		bad[6] = 'Y'
	}
	for _, in := range []string{string(bad), "U3M8Q2", "U3M8Q2KX", "U000000", ""} {
		if validCode(in) {
			t.Fatalf("%q 应判无效", in)
		}
	}
}
