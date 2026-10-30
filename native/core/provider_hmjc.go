package core

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (

	hmjcSiteBaseURL  = "https://www.kuaikaw.cn"
	hmjcPortalURL    = "https://freevideo.zqqds.cn/free-video-portal/portal"
	hmjcHMACKey      = "1pQ86jdDNQR584CmGICPnfdzydOLfU/EebtAKkZ5jkE="
	hmjcAppUA        = "okhttp/3.12.13"
	hmjcWebUA        = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	hmjcTmpID        = "00000000000000000000000000000001"
	hmjcPageSize     = 10
	hmjcAppTimeout   = 20 * time.Second
	hmjcWebTimeout   = 15 * time.Second
	hmjcRateLimitGap = time.Second
)

var reHmjcNextData = regexp.MustCompile(`(?is)<script id="__NEXT_DATA__"[^>]*>(.*?)</script>`)

type hmjcAppClient struct {
	utdid    string
	userID   string
	token    string
	mu       sync.Mutex
	lastCall time.Time
}

func hmjcCompactJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return out, nil
}

func hmjcSign(timestamp, nonce, body string) string {
	msg := timestamp + "\n" + nonce + "\n" + body
	mac := hmac.New(sha256.New, []byte(hmjcHMACKey))
	mac.Write([]byte(msg))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func hmjcNonce(n int) string {
	const pool = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = pool[rand.Intn(len(pool))]
	}
	return string(b)
}

func (c *hmjcAppClient) buildHeaders(body string) http.Header {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := hmjcNonce(16)
	datas := map[string]any{
		"freeflow": 0, "version": "3.12.0", "pname": "com.dz.hmjc",
		"channelCode": "HMJC1000000", "utdidTmp": c.utdid,
		"token": c.token, "utdid": c.utdid, "os": "android",
		"osv": 33, "brand": "Xiaomi", "model": "MI 8", "manu": "Xiaomi",
		"userId": c.userID, "session1": "", "session2": "",
		"ds": 0, "p": 65, "nonce": nonce, "timeZone": "8",
		"timestamp": timestamp, "recSwitch": true,
	}
	datasBytes, _ := hmjcCompactJSON(datas)
	h := http.Header{}
	h.Set("alg", "HG45LKBS")
	h.Set("sign", hmjcSign(timestamp, nonce, body))
	h.Set("datas", string(datasBytes))
	h.Set("wetruwtty", "mhdfiheowjfcslkjfwojo636")
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Accept", "*/*")
	h.Set("X-Request-ID", hmjcNonce(32))
	h.Set("User-Agent", hmjcAppUA)
	return h
}

func (d *Downloader) hmjcAppCall(ctx context.Context, code string, params map[string]any) (map[string]any, error) {
	cli := d.getHmjcAppClient(ctx)
	if cli == nil {
		return nil, errors.New("河马剧场 App 网关不可用")
	}
	cli.mu.Lock()
	gap := hmjcRateLimitGap - time.Since(cli.lastCall)
	cli.mu.Unlock()
	if gap > 0 {
		select {
		case <-time.After(gap):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	bodyBytes, err := hmjcCompactJSON(params)
	if err != nil {
		return nil, err
	}
	body := string(bodyBytes)
	address := hmjcPortalURL + "/" + code
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header = cli.buildHeaders(body)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	cli.mu.Lock()
	cli.lastCall = time.Now()
	cli.mu.Unlock()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("portal/%s HTTP %d", code, resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > providerMaxBodyBytes {
		return nil, fmt.Errorf("portal/%s 响应过大", code)
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("portal/%s JSON 解析失败: %w", code, err)
	}
	return result, nil
}

func (d *Downloader) getHmjcAppClient(ctx context.Context) *hmjcAppClient {
	d.hmjcOnce.Do(func() {
		cli := &hmjcAppClient{utdid: hmjcNonce(32)}
		j, err := d.hmjcAppCallRaw(ctx, "1103", map[string]any{}, cli)
		if err != nil || hmjcCode(j) != 0 {
			d.hmjcFailed = true
			return
		}
		uid := ""
		if v, ok := j["userId"].(string); ok {
			uid = v
		}
		if info, ok := j["userInfoVo"].(map[string]any); ok {
			if v, ok := info["userId"].(string); ok && v != "" {
				uid = v
			}
		} else if data, ok := j["data"].(map[string]any); ok {
			if info, ok := data["userInfoVo"].(map[string]any); ok {
				if v, ok := info["userId"].(string); ok && v != "" {
					uid = v
				}
			}
		}
		if uid == "" {
			d.hmjcFailed = true
			return
		}
		cli.userID = uid
		d.hmjc = cli
	})
	if d.hmjcFailed {
		return nil
	}
	return d.hmjc
}

func (d *Downloader) hmjcAppCallRaw(ctx context.Context, code string, params map[string]any, cli *hmjcAppClient) (map[string]any, error) {
	bodyBytes, err := hmjcCompactJSON(params)
	if err != nil {
		return nil, err
	}
	body := string(bodyBytes)
	address := hmjcPortalURL + "/" + code
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header = cli.buildHeaders(body)
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("portal/%s HTTP %d", code, resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func hmjcCode(j map[string]any) int {
	v, _ := j["code"].(float64)
	return int(v)
}

func hmjcData(j map[string]any) map[string]any {
	if data, ok := j["data"].(map[string]any); ok {
		return data
	}
	return map[string]any{}
}

func (d *Downloader) hmjcAppChapters(ctx context.Context, bookID string) ([]map[string]any, error) {
	j, err := d.hmjcAppCall(ctx, "1132", map[string]any{"bookId": bookID})
	if err != nil || hmjcCode(j) != 0 {
		return nil, fmt.Errorf("1132 失败: %w", err)
	}
	if list, ok := hmjcData(j)["chapterList"].([]any); ok {
		out := make([]map[string]any, 0, len(list))
		for _, item := range list {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out, nil
	}
	return nil, nil
}

func (d *Downloader) hmjcAppVideo(ctx context.Context, bookID, chapterID string) ([]string, error) {
	j, err := d.hmjcAppCall(ctx, "1131", map[string]any{"bookId": bookID, "chapterId": chapterID})
	if err != nil || hmjcCode(j) != 0 {
		return nil, fmt.Errorf("1131 失败: %w", err)
	}
	vi, _ := hmjcData(j)["videoInfo"].(map[string]any)
	content, _ := vi["content"].(map[string]any)
	raw, _ := content["mp4SwitchUrl"].([]any)
	var urls []string
	for _, u := range raw {
		if s, ok := u.(string); ok && strings.HasPrefix(s, "http") {
			urls = append(urls, s)
		}
	}
	return urls, nil
}

func (d *Downloader) hmjcWebPost(ctx context.Context, code string, payload map[string]any) (map[string]any, error) {
	bodyBytes, err := hmjcCompactJSON(payload)
	if err != nil {
		return nil, err
	}
	address := hmjcSiteBaseURL + "/seo/video/" + code
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", hmjcWebUA)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("pname", "www.kuaikaw.cn")
	req.Header.Set("tmpid", hmjcTmpID)
	req.Header.Set("Origin", hmjcSiteBaseURL)
	req.Header.Set("Referer", hmjcSiteBaseURL+"/")
	req.Header.Set("Accept", "*/*")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("seo/%s HTTP %d", code, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > providerMaxBodyBytes {
		return nil, fmt.Errorf("seo/%s 响应过大", code)
	}
	var j map[string]any
	if err := json.Unmarshal(data, &j); err != nil {
		return nil, err
	}
	if retCode, _ := j["retCode"].(float64); retCode != 0 {
		retMsg, _ := j["retMsg"].(string)
		return nil, fmt.Errorf("seo/%s retCode=%v %s", code, retCode, retMsg)
	}
	if result, ok := j["data"].(map[string]any); ok {
		return result, nil
	}
	return map[string]any{}, nil
}

func (d *Downloader) hmjcWebGet(ctx context.Context, path string) (string, error) {
	address := hmjcSiteBaseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", hmjcWebUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,*/*")
	req.Header.Set("Referer", hmjcSiteBaseURL+"/")
	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%s HTTP %d", path, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return "", err
	}
	if len(body) > providerMaxBodyBytes {
		return "", fmt.Errorf("%s 响应过大", path)
	}
	return string(body), nil
}

func hmjcNextDataPageProps(body string) map[string]any {
	m := reHmjcNextData.FindStringSubmatch(body)
	if len(m) < 2 {
		return map[string]any{}
	}
	var j map[string]any
	if json.Unmarshal([]byte(m[1]), &j) != nil {
		return map[string]any{}
	}
	if props, ok := j["props"].(map[string]any); ok {
		if page, ok := props["pageProps"].(map[string]any); ok {
			return page
		}
	}
	return map[string]any{}
}

func hmjcDramaFromRow(row map[string]any, category string) (Drama, bool) {
	bookID := strings.TrimSpace(mapString(row, "bookId"))
	if bookID == "" {
		return Drama{}, false
	}
	total := strings.TrimSpace(mapString(row, "totalChapterNum"))
	remark := ""
	if total != "" {
		remark = "全" + total + "集"
	}
	if remark == "" {
		remark = strings.TrimSpace(mapString(row, "statusDesc"))
	}
	cover := strings.TrimSpace(mapString(row, "coverWap"))
	title := strings.TrimSpace(mapString(row, "bookName"))
	return Drama{
		ID:           providerDramaID(sourceHmjc, bookID),
		Source:       sourceHmjc,
		SourceID:     bookID,
		Title:        truncate(title, 512),
		Name:         truncate(title, 512),
		Cover:        cover,
		CoverURL:     cover,
		ChannelName:  "河马剧场",
		CategoryName: category,
		Remark:       truncate(remark, 64),
	}, true
}

func hmjcDramaList(rows []any, category string) []Drama {
	var out []Drama
	seen := map[string]bool{}
	for _, row := range rows {
		if r, ok := row.(map[string]any); ok {
			if dr, ok := hmjcDramaFromRow(r, category); ok && dr.Title != "" && !seen[dr.SourceID] {
				seen[dr.SourceID] = true
				out = append(out, dr)
			}
		}
	}
	return out
}

func validHmjcCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 128 || strings.ContainsAny(category, "|\\\x00\r\n") {
		return false
	}
	return true
}

func (d *Downloader) fetchHmjcCategories(ctx context.Context) ([]nativeCategory, error) {
	data, err := d.hmjcWebPost(ctx, "6000", map[string]any{})
	if err != nil {
		return nil, err
	}
	var out []nativeCategory
	if list, ok := data["typeThreeList"].([]any); ok {
		for _, item := range list {
			if c, ok := item.(map[string]any); ok {
				name := strings.TrimSpace(mapString(c, "name"))
				var ids []string
				if raw, ok := c["typeThreeIds"].([]any); ok {
					for _, v := range raw {
						ids = append(ids, fmt.Sprint(v))
					}
				}
				if name != "" && len(ids) > 0 {
					out = append(out, nativeCategory{ID: strings.Join(ids, ","), Name: name})
				}
			}
		}
	}
	return out, nil
}

func (d *Downloader) fetchHmjcHome(ctx context.Context) []Drama {
	data, err := d.hmjcWebPost(ctx, "6000", map[string]any{})
	if err != nil {
		return nil
	}
	var out []Drama
	seen := map[string]bool{}
	if cols, ok := data["seoColumnVos"].([]any); ok {
		for _, col := range cols {
			if c, ok := col.(map[string]any); ok {
				if books, ok := c["bookInfos"].([]any); ok {
					for _, dr := range hmjcDramaList(books, "") {
						if !seen[dr.SourceID] {
							seen[dr.SourceID] = true
							out = append(out, dr)
						}
					}
				}
			}
		}
	}
	return out
}

func (d *Downloader) fetchHmjcCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if query != "" {
		return d.searchHmjc(ctx, page, query)
	}
	if category == "" {
		items := d.fetchHmjcHome(ctx)
		return items, false, nil
	}
	ids := strings.Split(category, ",")
	var all []Drama
	seen := map[string]bool{}
	maxPages := 1
	for _, one := range ids {
		one = strings.TrimSpace(one)
		if one == "" {
			continue
		}
		body, err := d.hmjcWebGet(ctx, fmt.Sprintf("/browse/%s/%d", one, page))
		if err != nil {
			continue
		}
		props := hmjcNextDataPageProps(body)
		if books, ok := props["bookList"].([]any); ok {
			for _, dr := range hmjcDramaList(books, "") {
				if !seen[dr.SourceID] {
					seen[dr.SourceID] = true
					all = append(all, dr)
				}
			}
		}
		if p, ok := props["pages"].(float64); ok && int(p) > maxPages {
			maxPages = int(p)
		}
	}
	return all, page < maxPages, nil
}

func (d *Downloader) searchHmjc(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, false, errors.New("河马剧场搜索关键词无效")
	}
	data, err := d.hmjcWebPost(ctx, "6007", map[string]any{
		"sourceType": 1, "keyword": query, "index": page,
	})
	if err != nil {
		return nil, false, err
	}
	var items []Drama
	if books, ok := data["bookList"].([]any); ok {
		items = hmjcDramaList(books, "")
	}
	total := 0
	if v, ok := data["totalSize"].(float64); ok {
		total = int(v)
	}
	pagecount := page
	if total > 0 {
		pagecount = (total + hmjcPageSize - 1) / hmjcPageSize
	}
	if pagecount < page {
		pagecount = page
	}
	return items, page < pagecount, nil
}

func hmjcChapterList(rows []any) []map[string]any {
	var out []map[string]any
	for _, row := range rows {
		if r, ok := row.(map[string]any); ok {
			cid := strings.TrimSpace(fmt.Sprint(r["chapterId"]))
			if cid != "" {
				out = append(out, r)
			}
		}
	}
	return out
}

func (d *Downloader) fetchHmjcDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return Drama{}, nil, errors.New("河马剧场视频 ID 无效")
	}
	props, err := d.hmjcWebPost(ctx, "6002", map[string]any{"bookId": sourceID, "type": 1})
	if err != nil {
		props = map[string]any{}
	}
	info, _ := props["bookInfoVo"].(map[string]any)
	if info == nil {
		body, webErr := d.hmjcWebGet(ctx, "/drama/"+sourceID)
		if webErr == nil {
			props = hmjcNextDataPageProps(body)
			info, _ = props["bookInfoVo"].(map[string]any)
		}
	}
	var chapterRows []map[string]any
	if rows, ok := props["chapterList"].([]any); ok {
		chapterRows = hmjcChapterList(rows)
	}
	if len(chapterRows) == 0 {
		if appRows, appErr := d.hmjcAppChapters(ctx, sourceID); appErr == nil {
			chapterRows = appRows
		}
	}
	name := strings.TrimSpace(mapString(info, "bookName"))
	if name == "" {
		name = sourceID
	}
	cover := strings.TrimSpace(mapString(info, "coverWap"))
	total := strings.TrimSpace(mapString(info, "totalChapterNum"))
	remark := ""
	if total != "" {
		remark = "全" + total + "集"
	}
	if remark == "" && len(chapterRows) > 0 {
		remark = fmt.Sprintf("%d集", len(chapterRows))
	}
	var tags []string
	if typeList, ok := info["bookTypeThree"].([]any); ok {
		for _, t := range typeList {
			if m, ok := t.(map[string]any); ok {
				if n := strings.TrimSpace(mapString(m, "name")); n != "" {
					tags = append(tags, n)
				}
			}
		}
	}
	drama := Drama{
		ID:           providerDramaID(sourceHmjc, sourceID),
		Source:       sourceHmjc,
		SourceID:     sourceID,
		Title:        truncate(name, 512),
		Name:         truncate(name, 512),
		Cover:        cover,
		CoverURL:     cover,
		ChannelName:  "河马剧场",
		CategoryName: strings.Join(tags, " "),
		Remark:       truncate(remark, 64),
		Desc:         truncate(strings.TrimSpace(mapString(info, "introduction")), 12000),
		Intro:        truncate(strings.TrimSpace(mapString(info, "introduction")), 12000),
		Tags:         tags,
	}
	var chapters []Chapter
	for idx, row := range chapterRows {
		cid := strings.TrimSpace(fmt.Sprint(row["chapterId"]))
		if cid == "" {
			continue
		}
		cname := strings.TrimSpace(fmt.Sprint(row["chapterName"]))
		order := idx + 1
		if v, ok := row["chapterIndex"].(float64); ok && int(v) > 0 {
			order = int(v)
		}
		if cname == "" {
			cname = fmt.Sprintf("第%d集", order)
		}
		chapters = append(chapters, Chapter{
			ID:             providerChapterID(sourceHmjc, sourceID, cid),
			Source:         sourceHmjc,
			Title:          truncate(cname, 128),
			CurrentEpisode: rawEpisode(order),
			PageURL:        cid,
			Referer:        hmjcSiteBaseURL + "/",
		})
	}
	sortProviderChapters(chapters)
	drama.TotalEpisode = len(chapters)
	drama.EpisodeCount = len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) resolveHmjcMedia(ctx context.Context, task Task) (providerMedia, error) {
	_, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid {
		return providerMedia{}, errors.New("河马剧场播放信息无效")
	}
	chapterID := task.Chapter.PageURL
	if chapterID == "" {
		prefix := providerChapterID(sourceHmjc, sourceID, "")
		chapterID = strings.TrimPrefix(task.Chapter.ID, prefix)
	}
	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		return providerMedia{}, errors.New("河马剧场集信息无效，请刷新详情")
	}
	urls, err := d.hmjcAppVideo(ctx, sourceID, chapterID)
	if err != nil || len(urls) == 0 {
		return providerMedia{}, errors.New("河马剧场该集暂无可用播放地址")
	}
	media := providerMedia{URL: urls[0], Referer: hmjcSiteBaseURL + "/"}
	parsed, err := url.Parse(urls[0])
	if err != nil {
		return providerMedia{}, errors.New("河马剧场播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "河马剧场")
	}
	return media, nil
}