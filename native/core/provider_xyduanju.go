package core

import (
	"bytes"
	"context"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	xyduanjuSiteURL  = "https://app.whjzjx.cn"
	xyduanjuLoginURL = "https://u.shytkjgs.com/user/v3/account/login"
	xyduanjuAESKey   = "B@ecf920Od8A4df7"
	xyduanjuPageSize = 24
)

var (
	xyduanjuTokenOnce sync.Once
	xyduanjuToken     string
	xyduanjuTokenOK   bool
)

func xyduanjuAESEncrypt(plainText string) (string, error) {
	block, err := aes.NewCipher([]byte(xyduanjuAESKey))
	if err != nil {
		return "", err
	}
	padding := 16 - len(plainText)%16
	padded := plainText + strings.Repeat(string(rune(padding)), padding)
	encrypted := make([]byte, len(padded))
	for i := 0; i < len(padded); i += 16 {
		block.Encrypt(encrypted[i:i+16], []byte(padded[i:i+16]))
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}

func (d *Downloader) xyduanjuLogin(ctx context.Context) bool {
	xyduanjuTokenOnce.Do(func() {
		data := map[string]any{
			"device": "2a50580e69d38388c94c93605241fb306",
			"package_name": "com.jz.xydj", "android_id": "ec1280db12795506",
			"install_first_open": true, "first_install_time": 1752505243345,
			"last_update_time": 1752505243345, "report_link_url": "",
			"authorization": "", "timestamp": time.Now().UnixMilli(),
		}
		raw, _ := json.Marshal(data)
		encrypted, err := xyduanjuAESEncrypt(string(raw))
		if err != nil {
			return
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, xyduanjuLoginURL, bytes.NewReader([]byte(encrypted)))
		if err != nil {
			return
		}
		req.Header.Set("platform", "1")
		req.Header.Set("user_agent", "Mozilla/5.0 (Linux; Android 9; V1938T)")
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		resp, err := d.client.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
		var j map[string]any
		if json.Unmarshal(payload, &j) != nil {
			return
		}
		if dataObj, ok := j["data"].(map[string]any); ok {
			if t, ok := dataObj["token"].(string); ok && t != "" {
				xyduanjuToken = t
				xyduanjuTokenOK = true
			}
		}
	})
	return xyduanjuTokenOK
}

func (d *Downloader) xyduanjuHeaders() http.Header {
	h := http.Header{}
	h.Set("platform", "1")
	h.Set("version_name", "3.8.3.1")
	if xyduanjuTokenOK {
		h.Set("authorization", xyduanjuToken)
	}
	return h
}

func (d *Downloader) xyduanjuGet(ctx context.Context, rawURL string) (map[string]any, error) {
	if !d.xyduanjuLogin(ctx) {
		return nil, errors.New("星芽登录失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = d.xyduanjuHeaders()
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("星芽 HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > providerMaxBodyBytes {
		return nil, errors.New("星芽响应过大")
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("星芽 JSON 解析失败: %w", err)
	}
	return result, nil
}

func (d *Downloader) xyduanjuPost(ctx context.Context, rawURL, body string) (map[string]any, error) {
	if !d.xyduanjuLogin(ctx) {
		return nil, errors.New("星芽登录失败")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, err
	}
	req.Header = d.xyduanjuHeaders()
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("星芽 HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > providerMaxBodyBytes {
		return nil, errors.New("星芽响应过大")
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("星芽 JSON 解析失败: %w", err)
	}
	return result, nil
}

func xyduanjuDramaList(list []any) []Drama {
	var out []Drama
	seen := map[string]bool{}
	for _, item := range list {
		v, ok := item.(map[string]any)
		if !ok {
			continue
		}
		theater, ok := v["theater"].(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(theater["id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		title := strings.TrimSpace(mapString(theater, "title"))
		if title == "" {
			continue
		}
		cover := strings.TrimSpace(mapString(theater, "cover_url"))
		remark := strings.TrimSpace(mapString(theater, "play_amount_str"))
		out = append(out, Drama{
			ID: providerDramaID(sourceXyduanju, id),
			Source: sourceXyduanju, SourceID: id,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover,
			ChannelName: "星芽", CategoryName: "短剧",
			Remark: truncate(remark, 64),
		})
	}
	return out
}

func (d *Downloader) fetchXyduanjuCategories() []nativeCategory {
	return []nativeCategory{
		{ID: "1", Name: "剧场"},
		{ID: "3", Name: "新剧"},
		{ID: "2", Name: "热播"},
		{ID: "7", Name: "星选"},
		{ID: "5", Name: "阳光"},
	}
}

func validXyduanjuCategory(category string) bool {
	if category == "" {
		return true
	}
	for _, id := range []string{"1", "2", "3", "5", "7"} {
		if category == id {
			return true
		}
	}
	return false
}

func (d *Downloader) fetchXyduanjuCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if query != "" {
		return d.searchXyduanju(ctx, page, query)
	}
	if category == "" {
		category = "1"
	}
	u := fmt.Sprintf("%s/v1/theater/home_page?theater_class_id=%s&page_num=%d&page_size=%d", xyduanjuSiteURL, category, page, xyduanjuPageSize)
	j, err := d.xyduanjuGet(ctx, u)
	if err != nil {
		return nil, false, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return nil, false, nil
	}
	list, _ := data["list"].([]any)
	items := xyduanjuDramaList(list)
	return items, len(items) >= xyduanjuPageSize, nil
}

func (d *Downloader) searchXyduanju(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	body, _ := json.Marshal(map[string]string{"text": query})
	j, err := d.xyduanjuPost(ctx, xyduanjuSiteURL+"/v3/search", string(body))
	if err != nil {
		return nil, false, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return nil, false, nil
	}
	theater, _ := data["theater"].(map[string]any)
	if theater == nil {
		return nil, false, nil
	}
	searchData, _ := theater["search_data"].([]any)
	var out []Drama
	seen := map[string]bool{}
	for _, item := range searchData {
		v, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(v["id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		title := strings.TrimSpace(mapString(v, "title"))
		if title == "" {
			continue
		}
		cover := strings.TrimSpace(mapString(v, "cover_url"))
		remark := strings.TrimSpace(mapString(v, "score_str"))
		out = append(out, Drama{
			ID: providerDramaID(sourceXyduanju, id),
			Source: sourceXyduanju, SourceID: id,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover,
			ChannelName: "星芽短剧", CategoryName: "短剧",
			Remark: truncate(remark, 64),
		})
	}
	return out, false, nil
}

func (d *Downloader) fetchXyduanjuDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return Drama{}, nil, errors.New("星芽剧集 ID 无效")
	}
	u := fmt.Sprintf("%s/v2/theater_parent/detail?theater_parent_id=%s", xyduanjuSiteURL, sourceID)
	j, err := d.xyduanjuGet(ctx, u)
	if err != nil {
		return Drama{}, nil, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return Drama{}, nil, errors.New("星芽详情响应无效")
	}
	name := strings.TrimSpace(mapString(data, "title"))
	if name == "" {
		name = sourceID
	}
	cover := strings.TrimSpace(mapString(data, "cover_url"))
	intro := strings.TrimSpace(mapString(data, "introduction"))
	filing := strings.TrimSpace(mapString(data, "filing"))
	drama := Drama{
		ID: providerDramaID(sourceXyduanju, sourceID),
		Source: sourceXyduanju, SourceID: sourceID,
		Title: truncate(name, 512), Name: truncate(name, 512),
		Cover: cover, CoverURL: cover,
		ChannelName: "星芽短剧", CategoryName: "短剧",
		Remark: truncate(filing, 64),
		Desc: truncate(intro, 12000), Intro: truncate(intro, 12000),
	}
	theaters, _ := data["theaters"].([]any)
	var chapters []Chapter
	for idx, item := range theaters {
		v, ok := item.(map[string]any)
		if !ok {
			continue
		}
		num := strings.TrimSpace(fmt.Sprint(v["num"]))
		videoURL := strings.TrimSpace(mapString(v, "son_video_url"))
		if videoURL == "" {
			continue
		}
		order := idx + 1
		if n, err := strconv.Atoi(num); err == nil && n > 0 {
			order = n
		}
		cname := num
		if cname == "" {
			cname = fmt.Sprintf("第%d集", order)
		}
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceXyduanju, sourceID, num),
			Source: sourceXyduanju,
			Title: truncate(cname, 128),
			CurrentEpisode: rawEpisode(order),
			VideoURL: videoURL,
			PageURL: num,
			Referer: xyduanjuSiteURL + "/",
		})
	}
	if len(chapters) == 0 {
		if videoURL := strings.TrimSpace(mapString(data, "video_url")); videoURL != "" {
			chapters = append(chapters, Chapter{
				ID: providerChapterID(sourceXyduanju, sourceID, "1"),
				Source: sourceXyduanju,
				Title: "第1集",
				CurrentEpisode: rawEpisode(1),
				VideoURL: videoURL,
				PageURL: "1",
				Referer: xyduanjuSiteURL + "/",
			})
		}
	}
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("星芽分集列表为空")
	}
	sortProviderChapters(chapters)
	drama.TotalEpisode = len(chapters)
	drama.EpisodeCount = len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) resolveXyduanjuMedia(ctx context.Context, task Task) (providerMedia, error) {
	mediaURL := strings.TrimSpace(task.Chapter.VideoURL)
	if mediaURL == "" {
		return providerMedia{}, errors.New("星芽该集暂无可用播放地址")
	}
	if !isProviderHTTPMediaURL(mediaURL) {
		return providerMedia{}, errors.New("星芽播放地址无效")
	}
	media := providerMedia{URL: mediaURL, Referer: xyduanjuSiteURL + "/"}
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return providerMedia{}, errors.New("星芽播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "星芽短剧")
	}
	return media, nil
}