package app

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/wechat-article/wechat-article-exporter/cli/internal/network"
	"github.com/wechat-article/wechat-article-exporter/cli/internal/wechat"
)

const (
	cleanRoomEnvironment = "WECHAT_ARTICLE_CLEAN_ROOM"
	cleanRoomOrigin      = "WECHAT_ARTICLE_CONTROLLED_ORIGIN"
)

type cleanRoomEnvironmentConfiguration struct {
	origin *url.URL
	policy network.DestinationPolicy
}

func controlledCleanRoomDependencies() (cleanRoomEnvironmentConfiguration, bool, error) {
	if strings.TrimSpace(os.Getenv(cleanRoomEnvironment)) != "1" {
		return cleanRoomEnvironmentConfiguration{}, false, nil
	}
	portableRoot := strings.TrimSpace(os.Getenv("WECHAT_ARTICLE_PORTABLE_ROOT"))
	if portableRoot == "" {
		return cleanRoomEnvironmentConfiguration{}, false, errors.New("WECHAT_ARTICLE_CLEAN_ROOM requires an isolated WECHAT_ARTICLE_PORTABLE_ROOT")
	}
	if !filepath.IsAbs(portableRoot) || filepath.Clean(portableRoot) == string(filepath.Separator) {
		return cleanRoomEnvironmentConfiguration{}, false, errors.New("WECHAT_ARTICLE_CLEAN_ROOM requires an absolute non-root WECHAT_ARTICLE_PORTABLE_ROOT")
	}
	origin, err := wechat.ParseControlledOrigin(os.Getenv(cleanRoomOrigin))
	if err != nil {
		return cleanRoomEnvironmentConfiguration{}, false, errors.New("WECHAT_ARTICLE_CLEAN_ROOM requires a literal loopback HTTP WECHAT_ARTICLE_CONTROLLED_ORIGIN with an explicit port and without path, query, fragment, or user information")
	}
	host := strings.ToLower(origin.Hostname())
	authority := strings.ToLower(origin.Host)
	return cleanRoomEnvironmentConfiguration{
		origin: origin,
		policy: network.DestinationPolicy{AllowedHosts: map[string]struct{}{host: {}}, AllowedAuthorities: map[string]struct{}{authority: {}}, AllowLoopback: true},
	}, true, nil
}
