// Package version 提供构建期注入的版本信息。
package version

import "runtime"

// Version 由 goreleaser / CI 通过 -ldflags -X 注入，缺省 dev。
var Version = "dev"

// Platform 返回当前运行平台描述。
func Platform() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}
