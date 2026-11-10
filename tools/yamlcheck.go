// 用法: go run /tmp/yamlcheck.go <file.yaml> [file2.yaml ...]
// 用 gopkg.in/yaml.v3 精确复现 docker-compose / backend setup 的
// "go-yaml load error in scanner (while scanning a simple key) ... could not find expected ':'"
// 错误，并打印出错位置 + 该行原文 + 前后几行，便于定位根因。
package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "用法: yamlcheck <file.yaml> [file2.yaml ...]")
		os.Exit(2)
	}
	fail := false
	for _, path := range os.Args[1:] {
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Printf("[读取失败] %s: %v\n", path, err)
			fail = true
			continue
		}
		lines := strings.Split(string(data), "\n")
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			fail = true
			fmt.Printf("\n❌ [语法错误] %s\n", path)
			fmt.Printf("   错误: %v\n", err)
			// 尝试从错误文本提取行号
			var ln int
			if _, e := fmt.Sscanf(err.Error(), "yaml: line %d:", &ln); e == nil && ln > 0 {
				printContext(lines, ln)
			} else {
				// 扫描含制表符缩进的行 —— yaml 最常见根因
				printTabHints(lines)
			}
		} else {
			fmt.Printf("✅ [OK] %s (%d 行, 可解析)\n", path, len(lines))
		}
	}
	if fail {
		os.Exit(1)
	}
}

func printContext(lines []string, ln int) {
	start := ln - 4
	if start < 0 {
		start = 0
	}
	end := ln + 2
	if end > len(lines) {
		end = len(lines)
	}
	fmt.Printf("   ═══ 第 %d 行附近上下文 ═══\n", ln)
	for i := start; i < end; i++ {
		marker := "  "
		if i+1 == ln {
			marker = ">>>"
		}
		vis := strings.ReplaceAll(lines[i], "\t", "⇥TAB")
		fmt.Printf("   %s %4d| %s\n", marker, i+1, vis)
	}
}

func printTabHints(lines []string) {
	for i, l := range lines {
		if strings.HasPrefix(l, "\t") || strings.Contains(l, "\t ") {
			vis := strings.ReplaceAll(l, "\t", "⇥TAB")
			fmt.Printf("   ⚠ 第 %d 行疑似制表符缩进: %s\n", i+1, vis)
		}
	}
}
