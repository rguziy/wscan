// Package version holds the build version, injected at link time:
//
//	go build -ldflags "-X github.com/rguziy/wscan/internal/version.Version=1.2.3"
package version

// Version is the wscan version. It is "dev" for a plain "go build".
var Version = "dev"
