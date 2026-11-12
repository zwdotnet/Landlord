package version

import "time"

// startedAt 记录本次进程启动时间，用于版本信息与运行诊断。
var startedAt = time.Now()

var (
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
	AppName   = "rental"
)

func GetVersion() map[string]string {
	return map[string]string{
		"version":   Version,
		"buildTime": BuildTime,
		"gitCommit": GitCommit,
		"appName":   AppName,
		"startedAt": startedAt.Format(time.RFC3339),
	}
}

func PrintVersion() {
	println(AppName, Version)
	println("Version:", Version)
	println("BuildTime:", BuildTime)
	println("GitCommit:", GitCommit)
}
