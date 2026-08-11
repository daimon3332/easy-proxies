package monitor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUpdateSettingsRejectsUnsupportedProbeTarget(t *testing.T) {
	s := &Server{}
	err := s.updateSettings("", "https://example.com/generate_204", false, nil, false)
	if err == nil {
		t.Fatal("expected unsupported probe target error")
	}
	if !strings.Contains(err.Error(), "探测目标只支持") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleFavicon(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).handleFavicon(recorder, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	if contentType := recorder.Header().Get("Content-Type"); contentType != "image/svg+xml" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if cacheControl := recorder.Header().Get("Cache-Control"); cacheControl != "public, max-age=86400" {
		t.Fatalf("unexpected cache control: %q", cacheControl)
	}
	if body := recorder.Body.String(); !strings.Contains(body, "<svg") || !strings.Contains(body, "Easy Proxies") {
		t.Fatalf("unexpected favicon body: %q", body)
	}
}

func TestHandleIndexIncludesRefreshProgressUI(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Server{}).handleIndex(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, marker := range []string{"easy_proxies_active_refresh_job", "failed_nodes", "refresh-modal", "probe_round_done", "重新检测部分完成", "正在应用节点和端口", "正在验证端口监听", "恢复检测前节点池", "测速已终止，检测前节点池已恢复", "return await retestNodes([id])", "dialog.dataset.refreshShell", "const scrollTop=listHost.scrollTop", "listHost.scrollTop=scrollTop"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("index is missing refresh UI marker %q", marker)
		}
	}
}

func TestWebUIKeepsDecisionDataAndRemovesRedundantCopy(t *testing.T) {
	data, err := embeddedFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(data)
	for _, required := range []string{
		`id="pageTitle" class="sr-only"`,
		`所选项目必须全部成功`,
		`export-tag-row`,
		`备份文件夹`,
		`默认 /easy_proxies`,
	} {
		if !strings.Contains(page, required) {
			t.Fatalf("WebUI missing required decision data %q", required)
		}
	}
	for _, removed := range []string{
		`粘贴订阅 URL，自动测速并生成本地端口`,
		`按 Tag 导出原始来源类型；多个 Tag 会自动打包为 ZIP。`,
		`完整保存设置、订阅、导入节点和节点池状态。`,
		`备份文件保存在当前设备；恢复前会校验文件，运行中的任务结束后才能恢复。`,
		`地址、账号和密码明文显示并保存，远程文件只写入指定目录。`,
		`默认检测 Google 204，失败时使用 Cloudflare 204 兜底。`,
		`只处理当前页面的节点；节点池/候选失败会进入失败节点，失败节点成功后按上方开关处理。`,
	} {
		if strings.Contains(page, removed) {
			t.Fatalf("WebUI still contains redundant copy %q", removed)
		}
	}
}
