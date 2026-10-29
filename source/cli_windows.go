//go:build windows && cli

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	fmt.Println("===================================================")
	fmt.Println(" WPS 嵌入图片转换工具 - Windows 备用命令行版 2.8")
	fmt.Println(" 无需安装 WPS / Office / Python")
	fmt.Println("===================================================")
	fmt.Println()
	inputs := os.Args[1:]
	opts := convertOptions{}
	if len(inputs) > 0 && inputs[0] == "--large" {
		opts.enlargeImageRows = true
		inputs = inputs[1:]
	}
	reader := bufio.NewReader(os.Stdin)
	if len(inputs) == 0 {
		fmt.Println("方法一：把 .xlsx 文件拖到本 EXE 图标上。")
		fmt.Println("方法二：复制 Excel 文件完整路径，在下面粘贴并回车。")
		fmt.Print("Excel 文件路径：")
		line, err := reader.ReadString('\n')
		if err != nil && len(line) == 0 {
			return
		}
		line = strings.TrimSpace(line)
		if len(line) >= 2 && line[0] == '"' && line[len(line)-1] == '"' {
			line = line[1 : len(line)-1]
		}
		if line == "" {
			return
		}
		inputs = []string{line}
	}
	success := 0
	for _, input := range inputs {
		input = strings.Trim(strings.TrimSpace(input), "\"")
		fmt.Printf("\n处理中：%s\n", filepath.Base(input))
		o := convertWorkbookWithOptions(input, opts)
		if o.err != nil {
			fmt.Printf("转换失败：%v\n", o.err)
			continue
		}
		success++
		fmt.Printf("完成！转换图片：%d 张，保持正确显示方向，未匹配：%d 张\n输出：%s\n", o.converted, o.unmatched, o.output)
		if o.converted == 0 {
			fmt.Println("提示：文件中没有找到能识别的 DISPIMG 图片，请核对原文件。")
		}
	}
	fmt.Printf("\n处理结束：成功 %d / %d 个文件。\n", success, len(inputs))
	fmt.Print("按回车键退出...")
	_, _ = reader.ReadString('\n')
}

