// Package main: dbverify CLI 运行入口的可执行回归——在临时 SQLite 上跑完整验证步骤。
package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunWithoutArguments 验证缺少数据库地址时打印用法并返回失败退出码。
func TestRunWithoutArguments(t *testing.T) {
	// out 是诊断输出缓冲。
	var out bytes.Buffer
	// code 是本次运行的退出码。
	if code := run(nil, &out); code != 1 {
		t.Fatalf("缺少参数应返回 1, got %d", code)
	}
	if !strings.Contains(out.String(), "用法") {
		t.Fatalf("应打印用法: %s", out.String())
	}
}

// TestRunOpenFailure 验证数据库地址不可用时报错并返回失败退出码。
func TestRunOpenFailure(t *testing.T) {
	// out 是诊断输出缓冲。
	var out bytes.Buffer
	// code 是本次运行的退出码；目录不存在时 Open 必然失败。
	code := run([]string{"sqlite://" + filepath.Join(t.TempDir(), "missing-dir", "verify.db")}, &out)
	if code != 1 || !strings.Contains(out.String(), "Open 失败") {
		t.Fatalf("不可用地址应失败: code=%d out=%s", code, out.String())
	}
}

// TestRunCompletesFullVerification 验证在临时 SQLite 上完成全部九步验证并清理临时数据。
func TestRunCompletesFullVerification(t *testing.T) {
	// dbPath 是本次运行的临时数据库文件。
	dbPath := filepath.Join(t.TempDir(), "verify.db")
	// url 是 sqlite 方言的数据库地址。
	url := "sqlite://" + dbPath
	// out 是诊断输出缓冲。
	var out bytes.Buffer
	// code 是本次运行的退出码。
	if code := run([]string{url}, &out); code != 0 {
		t.Fatalf("完整验证应成功: code=%d out=%s", code, out.String())
	}
	// 关键步骤的成功标记必须都出现，避免只验证了部分链路。
	// marker 是当前期望出现的成功标记。
	for _, marker := range []string{"迁移成功", "订单 upsert 成功", "商品信息 upsert 成功", "卡密组", "通知渠道 + 绑定 OK", "自动化规则 + 防重 OK", "验证数据已清理", "全部验证通过"} {
		if !strings.Contains(out.String(), marker) {
			t.Fatalf("输出缺少 %q: %s", marker, out.String())
		}
	}

	// 同一数据库重复运行必须成功：验证数据使用唯一标识，不会因残留数据冲突。
	// second 是第二次运行的诊断输出缓冲。
	var second bytes.Buffer
	// code 是本次运行的退出码。
	if code := run([]string{url}, &second); code != 0 {
		t.Fatalf("重复运行应成功: code=%d out=%s", code, second.String())
	}
	// 第二次运行同样必须清理干净。
	if !strings.Contains(second.String(), "验证数据已清理") {
		t.Fatalf("重复运行应清理验证数据: %s", second.String())
	}
}
