package module

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// 2026-11-01：私有仓凭据解析的判据。
// 背景：dc 的更新检查原先匿名取 manifest，私有仓必然 401 且**静默跳过**
// （连结果都不写），前端表现成"没有通知"。这里钉住三件事：
//  ① 各工具写法的 auths key 都能归一到同一形态
//  ② auth 与 username/password 两种凭据形态都能取到
//  ③ 找不到凭据时返回空串（=匿名），不能 panic、不能误用别的 registry 的凭据

func TestNormalizeRegistryKey(t *testing.T) {
	cases := map[string]string{
		"https://index.docker.io/v1/": "docker.io",
		"registry-1.docker.io":        "docker.io",
		"docker.io":                   "docker.io",
		"docker.io/":                  "docker.io",
		"https://ghcr.io":             "ghcr.io",
		"ghcr.io/":                    "ghcr.io",
		"quay.io":                     "quay.io",
	}
	for in, want := range cases {
		if got := normalizeRegistryKey(in); got != want {
			t.Errorf("normalizeRegistryKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRegistryOf(t *testing.T) {
	cases := map[string]string{
		"zchjoe/nc-bridge":            "docker.io", // 无域名 ⇒ Docker Hub
		"nginx":                       "docker.io",
		"docker.io/library/nginx":     "docker.io",
		"ghcr.io/foo/bar":             "ghcr.io",
		"registry.example.com:5000/x": "registry.example.com:5000",
	}
	for in, want := range cases {
		if got := registryOf(in); got != want {
			t.Errorf("registryOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLookupAuthPrefersAuthField(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte("u:p"))
	cfg := &dockerConfigFile{Auths: map[string]dockerConfigAuth{
		"https://index.docker.io/v1/": {Auth: want},
	}}
	got, ok := lookupAuth(cfg, "docker.io")
	if !ok || got != want {
		t.Fatalf("lookupAuth = (%q,%v), want (%q,true)", got, ok, want)
	}
}

func TestLookupAuthFallsBackToUserPassword(t *testing.T) {
	cfg := &dockerConfigFile{Auths: map[string]dockerConfigAuth{
		"ghcr.io": {Username: "u", Password: "p"},
	}}
	got, ok := lookupAuth(cfg, "ghcr.io")
	want := base64.StdEncoding.EncodeToString([]byte("u:p"))
	if !ok || got != want {
		t.Fatalf("lookupAuth = (%q,%v), want (%q,true)", got, ok, want)
	}
}

func TestLookupAuthMissReturnsEmpty(t *testing.T) {
	cfg := &dockerConfigFile{Auths: map[string]dockerConfigAuth{
		"ghcr.io": {Auth: "x"},
	}}
	if got, ok := lookupAuth(cfg, "docker.io"); ok || got != "" {
		t.Fatalf("不应把别的 registry 的凭据用到 docker.io 上：(%q,%v)", got, ok)
	}
}

func TestAuthForReadsConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	auth := base64.StdEncoding.EncodeToString([]byte("bot:token"))
	body := `{"auths":{"https://index.docker.io/v1/":{"auth":"` + auth + `"}}}`
	if err := os.WriteFile(cfgPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("DC_REGISTRY_AUTH", "")
	got, src := AuthForWithSource("zchjoe/nc-bridge")
	if got != auth {
		t.Fatalf("AuthFor = %q, want %q", got, auth)
	}
	if src != cfgPath {
		t.Errorf("来源应为 %s，实际 %s", cfgPath, src)
	}
}

func TestAuthForEnvOverride(t *testing.T) {
	t.Setenv("DC_REGISTRY_AUTH", "YWJjOmRlZg==") // abc:def
	got, src := AuthForWithSource("any/image")
	if got != "YWJjOmRlZg==" || src != "env:DC_REGISTRY_AUTH" {
		t.Fatalf("环境变量兜底失效：(%q,%q)", got, src)
	}
}

func TestAuthForMissingConfigIsAnonymous(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", filepath.Join(t.TempDir(), "nope"))
	t.Setenv("DC_REGISTRY_AUTH", "")
	if got := AuthFor("zchjoe/nc-bridge"); got != "" {
		t.Fatalf("没有配置文件时应返回空（匿名），实际 %q", got)
	}
}
