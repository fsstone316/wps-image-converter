# WPS 图片转 Excel v2.9

Windows 64 位工具：将 WPS 的 `DISPIMG` 单元格图片转换成 Microsoft Excel 兼容图片，无需安装 WPS、Python。

## 功能
- 导入单个或多个文件、文件夹及拖放文件，设置选项后点击“开始转换”
- 可选按原列宽统一图片宽度；保留宽高比
- 修复图片方向、锚点与 Excel 行高兼容性
- 处理记录列表：打开结果、定位文件夹、重命名及删除输出文件
- 原文件不覆盖，输出 `_Office兼容.xlsx` 或 `_Office兼容.xlsm`

## Windows 构建
安装 Go 1.23 或更新版本后：

```powershell
cd source
go test ./...
go build -ldflags="-H windowsgui" -o ..\WPS图片转Excel.exe .
go build -tags cli -o ..\WPS图片转Excel_命令行.exe .
```

在 GitHub Actions 中手动运行 [Build Windows EXE](../../actions/workflows/build-windows.yml)，构建后从 Artifacts 下载程序。

**注意**：仓库尚未包含原版 EXE 的图标资源文件，自动构建版本可能不显示自定义图标；2.9 原版完整绿色包仍可从聊天附件获取。
