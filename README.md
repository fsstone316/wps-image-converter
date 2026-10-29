# WPS Image Converter | WPS 图片转 Excel v2.9

Windows 64 位工具，将 WPS 的 `DISPIMG` 单元格图片转换为 Microsoft Excel 可识别的标准图片。**无需安装 WPS 或 Python。**

## 功能

- 文件、多文件和文件夹导入，支持拖放；**导入 → 设置选项 → 开始转换**
- 统一图片宽度（默认按所在列宽 100%）、等比例缩放
- 修复 WPS 图片方向、锚点定位、Excel 自动压缩行高问题
- 文件列表、处理状态、打开结果、打开文件夹、重命名及删除输出
- 不覆盖原文件；输出 `原文件名_Office兼容.xlsx`（或 `.xlsm`）

## 获取 Windows 可执行程序

进入 [Actions → Build Windows EXE](../../actions/workflows/build-windows.yml)，选择 **Run workflow**。构建完成后下载 Artifacts 中的 `WPS-Image-Converter-Windows64`。

> 源代码同步到 GitHub；预先编译的 2.9 EXE 暂未直接上传。GitHub Actions 可构建 GUI 和命令行 EXE。由于仓库目前不包含原版内嵌的二进制图标资源，Actions 构建版本可能没有自定义程序图标。

## 本地编译

需要 Go 1.23+，在 Windows 命令提示符或 PowerShell 运行：

```powershell
cd source
go test ./...
go build -ldflags="-H windowsgui" -o ..\WPS图片转Excel.exe .
go build -tags cli -o ..\WPS图片转Excel_命令行.exe .
```

请使用**原始 WPS Excel 文件**进行转换。关闭“统一图片宽度”选项可以沿用 2.7 版已验证的排版方式。

此仓库独立于 `stone-codex-skills`，未经用户提供的项目 Excel 文件均未上传。
