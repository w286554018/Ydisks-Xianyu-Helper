// Command tmp-export 从本地加密数据库导出指定账号的平台 Cookie 明文，供本地抓包工具使用。
// 临时工具：XIANYU_DATA_KEY 环境变量提供解密密钥，导出到指定文件并设置 0600 权限。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"xianyu-go/internal/db"
)

// main 解析命令行参数并完成 cookie 明文导出流程。
func main() {
	// dbPath 是 SQLite 数据库路径参数。
	dbPath := flag.String("db", "", "SQLite 数据库路径")
	// cookieID 是待导出账号 ID（cookie_id）参数。
	cookieID := flag.String("account", "", "账号 ID（cookie_id）")
	// out 是 Cookie 明文输出文件路径参数。
	out := flag.String("out", "", "输出 Cookie 明文文件路径")
	flag.Parse()

	if *dbPath == "" || *cookieID == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "用法: tmp-export -db <db> -account <id> -out <file>")
		os.Exit(2)
	}
	// ctx 保存导出流程共用的上下文。
	ctx := context.Background()
	// sqlDB、err 保存打开的数据库连接与错误。
	sqlDB, _, err := db.Open(ctx, *dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "打开数据库失败:", err)
		os.Exit(1)
	}
	defer sqlDB.Close()
	// store 是面向 SQLite 方言的仓库集合。
	store := db.NewStore(sqlDB, "sqlite")
	// value、err 保存解密后的 Cookie 明文与读取错误。
	value, err := store.Cookies.GetValue(ctx, *cookieID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "读取 Cookie 失败:", err)
		os.Exit(1)
	}
	// err 保存输出目录创建错误。
	if err := os.MkdirAll(filepath.Dir(*out), 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "创建输出目录失败:", err)
		os.Exit(1)
	}
	// err 保存 Cookie 明文写出错误。
	if err := os.WriteFile(*out, []byte(value), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "写出失败:", err)
		os.Exit(1)
	}
	fmt.Printf("OK account=%s bytes=%d out=%s\n", *cookieID, len(value), *out)
}
