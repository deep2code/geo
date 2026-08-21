// Command gen_app_settings_seed 从 config 设置注册表生成 app_settings 种子 SQL。
//
// 用法：go run ./scripts/gen_app_settings_seed
// 输出：INSERT IGNORE 语句块（含全部配置项的默认值），嵌入 deploy/initdb/schema.sql
// 的 app_settings 表定义之后。新增/修改配置项后重新运行并同步到 schema.sql。
package main

import (
	"fmt"
	"os"
	"strings"

	"my-geo/internal/config"
)

func main() {
	items := config.ListSettings()
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "catalog 为空")
		os.Exit(1)
	}
	var b strings.Builder
	b.WriteString("-- app_settings 种子数据（由 scripts/gen_app_settings_seed 生成，勿手改；")
	b.WriteString("新增配置项后重跑该工具并同步到本文件）\n")
	b.WriteString("INSERT IGNORE INTO app_settings\n")
	b.WriteString("  (skey, svalue, default_value, description, category, stype, is_secret, is_bootstrap, requires_restart, updated_at)\n")
	b.WriteString("VALUES\n")
	rows := make([]string, 0, len(items))
	for _, s := range items {
		// svalue = DefaultValue：默认值随建库植入，启动后由管理后台修改。
		// bootstrap 项（DSN/管理员/AUTH/JWT）默认值为空串，实际值由环境变量引导。
		rows = append(rows, fmt.Sprintf("  (%s, %s, %s, %s, %s, %s, %d, %d, %d, 0)",
			sqlStr(s.Key), sqlStr(s.DefaultValue), sqlStr(s.DefaultValue),
			sqlStr(s.Description), sqlStr(s.Category), sqlStr(s.Type),
			boolInt(s.IsSecret), boolInt(s.IsBootstrap), boolInt(s.RequiresRestart)))
	}
	b.WriteString(strings.Join(rows, ",\n"))
	b.WriteString(";\n")
	fmt.Print(b.String())
}

// sqlStr 转义为 MySQL 单引号字符串字面量。
func sqlStr(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
