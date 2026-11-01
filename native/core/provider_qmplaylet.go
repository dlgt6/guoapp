package core

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

const (
	qmplayletStoreURL = "https://api-store.qmplaylet.com"
	qmplayletReadURL  = "https://api-read.qmplaylet.com"
	qmplayletKeys     = "d3dGiJc651gSQ8w1"
	qmplayletTrackID  = "ec1280db127955061754851657967"
	qmplayletPageSize = 30
)

var (
	qmplayletHeaderOnce sync.Once
	qmplayletHeaders    http.Header
)

func qmplayletMD5(input string) string {
	sum := md5.Sum([]byte(input))
	return hex.EncodeToString(sum[:])
}

func qmplayletBuildHeaders() http.Header {
	qmplayletHeaderOnce.Do(func() {
		device := map[string]any{
			"static_score": "0.8", "uuid": "00000000-7fc7-08dc-0000-000000000000",
			"device-id":    "20250220125449b9b8cac84c2dd3d035c9052a2572f7dd0122edde3cc42a70",
			"mac": "", "sourceuid": "aa7de295aad621a6", "refresh-type": "0",
			"model": "22021211RC", "wlb-imei": "", "client-id": "aa7de295aad621a6",
			"brand": "Redmi", "oaid": "", "oaid-no-cache": "", "sys-ver": "12",
			"trusted-id": "", "phone-level": "H", "imei": "", "wlb-uid": "aa7de295aad621a6",
			"session-id": "1750000000000",
		}
		raw, _ := json.Marshal(device)
		encoded := base64.StdEncoding.EncodeToString(raw)
		mapping := map[rune]rune{
			'+': 'P', '/': 'X', '0': 'M', '1': 'U', '2': 'l', '3': 'E', '4': 'r',
			'5': 'Y', '6': 'W', '7': 'b', '8': 'd', '9': 'J', 'A': '9', 'B': 's',
			'C': 'a', 'D': 'I', 'E': '0', 'F': 'o', 'G': 'y', 'H': '_', 'I': 'H',
			'J': 'G', 'K': 'i', 'L': 't', 'M': 'g', 'N': 'N', 'O': 'A', 'P': '8',
			'Q': 'F', 'R': 'k', 'S': '3', 'T': 'h', 'U': 'f', 'V': 'R', 'W': 'q',
			'X': 'C', 'Y': '4', 'Z': 'p', 'a': 'm', 'b': 'B', 'c': 'O', 'd': 'u',
			'e': 'c', 'f': '6', 'g': 'K', 'h': 'x', 'i': '5', 'j': 'T', 'k': '-',
			'l': '2', 'm': 'z', 'n': 'S', 'o': 'Z', 'p': '1', 'q': 'V', 'r': 'v',
			's': 'j', 't': 'Q', 'u': '7', 'v': 'D', 'w': 'w', 'x': 'n', 'y': 'L',
			'z': 'e',
		}
		qmParams := make([]rune, 0, len(encoded))
		for _, c := range encoded {
			if r, ok := mapping[c]; ok {
				qmParams = append(qmParams, r)
			} else {
				qmParams = append(qmParams, c)
			}
		}
		qmParamsStr := string(qmParams)
		signStr := "AUTHORIZATION=app-version=10001application-id=com.duoduo.readchannel=unknownis-white=net-env=5platform=androidqm-params=" + qmParamsStr + "reg=" + qmplayletKeys
		h := http.Header{}
		h.Set("net-env", "5")
		h.Set("reg", "")
		h.Set("channel", "unknown")
		h.Set("is-white", "")
		h.Set("platform", "android")
		h.Set("application-id", "com.duoduo.read")
		h.Set("authorization", "")
		h.Set("app-version", "10001")
		h.Set("user-agent", "webviewversion/0")
		h.Set("qm-params", qmParamsStr)
		h.Set("sign", qmplayletMD5(signStr))
		qmplayletHeaders = h
	})
	return qmplayletHeaders.Clone()
}

func (d *Downloader) qmplayletGet(ctx context.Context, rawURL string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header = qmplayletBuildHeaders()
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("七猫 HTTP %d", resp.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, providerMaxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(payload) > providerMaxBodyBytes {
		return nil, errors.New("七猫响应过大")
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("七猫 JSON 解析失败: %w", err)
	}
	return result, nil
}

func qmplayletSign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(params[k])
	}
	b.WriteString(qmplayletKeys)
	return qmplayletMD5(b.String())
}

func (d *Downloader) fetchQmplayletCategories(ctx context.Context) ([]nativeCategory, error) {
	sign := qmplayletSign(map[string]string{"operation": "1", "playlet_privacy": "1", "tag_id": "0"})
	u := fmt.Sprintf("%s/api/v1/playlet/index?tag_id=0&playlet_privacy=1&operation=1&sign=%s", qmplayletStoreURL, sign)
	j, err := d.qmplayletGet(ctx, u)
	if err != nil {
		return nil, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return nil, errors.New("七猫分类响应无效")
	}
	var out []nativeCategory
	if cats, ok := data["tag_categories"].([]any); ok {
		for _, cat := range cats {
			if c, ok := cat.(map[string]any); ok {
				if tags, ok := c["tags"].([]any); ok {
					for _, t := range tags {
						if tag, ok := t.(map[string]any); ok {
							name := strings.TrimSpace(mapString(tag, "tag_name"))
							id := strings.TrimSpace(fmt.Sprint(tag["tag_id"]))
							if name == "" || strings.Contains(name, "推荐") || id == "" {
								continue
							}
							out = append(out, nativeCategory{ID: id, Name: name})
						}
					}
				}
			}
		}
	}
	return out, nil
}

func qmplayletDramaList(list []any) []Drama {
	var out []Drama
	seen := map[string]bool{}
	for _, item := range list {
		v, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(v["playlet_id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		title := strings.TrimSpace(mapString(v, "title"))
		if title == "" {
			continue
		}
		cover := strings.TrimSpace(mapString(v, "image_link"))
		out = append(out, Drama{
			ID: providerDramaID(sourceQmplaylet, id),
			Source: sourceQmplaylet, SourceID: id,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover,
			ChannelName: "七猫", CategoryName: "短剧",
		})
	}
	return out
}

func (d *Downloader) fetchQmplayletCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if query != "" {
		return d.searchQmplaylet(ctx, page, query)
	}
	if category == "" {
		category = "0"
	}
	params := map[string]string{"operation": "1", "playlet_privacy": "1", "tag_id": category}
	nextPart := ""
	if page > 1 {
		params["next_id"] = fmt.Sprint(page)
		nextPart = "&next_id=" + url.QueryEscape(fmt.Sprint(page))
	}
	sign := qmplayletSign(params)
	u := fmt.Sprintf("%s/api/v1/playlet/index?tag_id=%s%s&playlet_privacy=1&operation=1&sign=%s", qmplayletStoreURL, url.QueryEscape(category), nextPart, sign)
	j, err := d.qmplayletGet(ctx, u)
	if err != nil {
		return nil, false, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return nil, false, errors.New("七猫列表响应无效")
	}
	list, _ := data["list"].([]any)
	items := qmplayletDramaList(list)
	return items, len(items) >= qmplayletPageSize, nil
}

func (d *Downloader) searchQmplaylet(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	params := map[string]string{
		"extend": "", "page": fmt.Sprint(page),
		"read_preference": "0", "track_id": qmplayletTrackID, "wd": query,
	}
	sign := qmplayletSign(params)
	u := fmt.Sprintf("%s/api/v1/playlet/search?extend=&page=%d&wd=%s&read_preference=0&track_id=%s&sign=%s", qmplayletStoreURL, page, url.QueryEscape(query), qmplayletTrackID, sign)
	j, err := d.qmplayletGet(ctx, u)
	if err != nil {
		return nil, false, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return nil, false, nil
	}
	list, _ := data["list"].([]any)
	var out []Drama
	seen := map[string]bool{}
	for _, item := range list {
		v, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(v["id"]))
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		title := cleanText(mapString(v, "title"))
		if title == "" {
			continue
		}
		cover := strings.TrimSpace(mapString(v, "image_link"))
		remark := ""
		totalNum := strings.TrimSpace(fmt.Sprint(v["total_num"]))
		if totalNum != "" && totalNum != "0" {
			remark = totalNum + "集"
		}
		out = append(out, Drama{
			ID: providerDramaID(sourceQmplaylet, id),
			Source: sourceQmplaylet, SourceID: id,
			Title: truncate(title, 512), Name: truncate(title, 512),
			Cover: cover, CoverURL: cover,
			ChannelName: "七猫短剧", CategoryName: "短剧",
			Remark: truncate(remark, 64),
		})
	}
	return out, len(out) >= qmplayletPageSize, nil
}

func (d *Downloader) fetchQmplayletDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return Drama{}, nil, errors.New("七猫剧集 ID 无效")
	}
	sign := qmplayletSign(map[string]string{"playlet_id": sourceID})
	u := fmt.Sprintf("%s/player/api/v1/playlet/info?playlet_id=%s&sign=%s", qmplayletReadURL, url.QueryEscape(sourceID), sign)
	j, err := d.qmplayletGet(ctx, u)
	if err != nil {
		return Drama{}, nil, err
	}
	data, _ := j["data"].(map[string]any)
	if data == nil {
		return Drama{}, nil, errors.New("七猫详情响应无效")
	}
	name := strings.TrimSpace(mapString(data, "title"))
	if name == "" {
		name = sourceID
	}
	cover := strings.TrimSpace(mapString(data, "image_link"))
	intro := strings.TrimSpace(mapString(data, "intro"))
	total := strings.TrimSpace(fmt.Sprint(data["total_episode_num"]))
	tags := strings.TrimSpace(mapString(data, "tags"))
	remark := tags
	if total != "" && total != "0" {
		if remark != "" {
			remark += " "
		}
		remark += total + "集"
	}
	drama := Drama{
		ID: providerDramaID(sourceQmplaylet, sourceID),
		Source: sourceQmplaylet, SourceID: sourceID,
		Title: truncate(name, 512), Name: truncate(name, 512),
		Cover: cover, CoverURL: cover,
		ChannelName: "七猫短剧", CategoryName: "短剧",
		Remark: truncate(remark, 64),
		Desc: truncate(intro, 12000), Intro: truncate(intro, 12000),
	}
	playList, _ := data["play_list"].([]any)
	var chapters []Chapter
	for idx, item := range playList {
		v, ok := item.(map[string]any)
		if !ok {
			continue
		}
		sortStr := strings.TrimSpace(fmt.Sprint(v["sort"]))
		videoURL := strings.TrimSpace(mapString(v, "video_url"))
		if videoURL == "" {
			continue
		}
		order := idx + 1
		if n, err := fmt.Sscanf(sortStr, "%d", &order); err != nil || n == 0 {
			order = idx + 1
		}
		cname := sortStr
		if cname == "" {
			cname = fmt.Sprintf("第%d集", order)
		}
		chapters = append(chapters, Chapter{
			ID: providerChapterID(sourceQmplaylet, sourceID, sortStr),
			Source: sourceQmplaylet,
			Title: truncate(cname, 128),
			CurrentEpisode: rawEpisode(order),
			VideoURL: videoURL,
			PageURL: sortStr,
			Referer: qmplayletStoreURL + "/",
		})
	}
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("七猫分集列表为空")
	}
	sortProviderChapters(chapters)
	drama.TotalEpisode = len(chapters)
	drama.EpisodeCount = len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) resolveQmplayletMedia(ctx context.Context, task Task) (providerMedia, error) {
	mediaURL := strings.TrimSpace(task.Chapter.VideoURL)
	if mediaURL == "" {
		return providerMedia{}, errors.New("七猫该集暂无可用播放地址")
	}
	if !isProviderHTTPMediaURL(mediaURL) {
		return providerMedia{}, errors.New("七猫播放地址无效")
	}
	media := providerMedia{URL: mediaURL, Referer: qmplayletStoreURL + "/"}
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return providerMedia{}, errors.New("七猫播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "七猫短剧")
	}
	return media, nil
}

func validQmplayletCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 128 || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	return true
}