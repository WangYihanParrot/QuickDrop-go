$name = "QuickDrop-go"
$version = "1.1"

# Windows
go build -o "./build/${name}_v${version}_windows_amd64.exe" main.go

# Linux amd64
$env:GOOS = "linux"; $env:GOARCH = "amd64"
go build -o "./build/${name}_v${version}_linux_amd64" main.go

# macOS Intel
$env:GOOS = "darwin"; $env:GOARCH = "amd64"
go build -o "./build/${name}_v${version}_darwin_amd64" main.go

# macOS ARM (M1/M2)
$env:GOOS = "darwin"; $env:GOARCH = "arm64"
go build -o "./build/${name}_v${version}_darwin_arm64" main.go

# 清理环境变量
Remove-Item Env:\GOOS
Remove-Item Env:\GOARCH


pause