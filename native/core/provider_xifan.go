package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	xifanSiteBaseURL = "https://xifan-api-cn.youlishipin.com"
	xifanUserAgent   = "Mozilla/5.0"
	xifanPageSize    = 30
	xifanGenreTTL    = 5 * time.Minute
)

var (
	reXifanID      = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,200}$`)
	reXifanHTMLTag = regexp.MustCompile(`<[^>]*>`)

	xifanGenreCache = xifanGenreCacheState{}
)

type xifanGenreCacheState struct {
	mu     sync.Mutex
	items  []xifanGenre
	expiry time.Time
}

type xifanGenre struct {
	Slug       string
	Label      string
	CategoryID int
	Version    int
}

type xifanPortalResult struct {
	Elements []struct {
		Contents []json.RawMessage `json:"contents"`
	} `json:"elements"`
	HasMore bool `json:"hasMore"`
}

type xifanCategoryItem struct {
	CategoryID     int                 `json:"categoryId"`
	OppoCategory   string              `json:"oppoCategory"`
	Version        int                 `json:"version"`
	SubCategories  []xifanCategoryItem `json:"subCategories"`
}

type xifanDuanjuVo struct {
	Source         string   `json:"source"`
	DuanjuID       string   `json:"duanjuId"`
	Title          string   `json:"title"`
	CoverImageURL  string   `json:"coverImageUrl"`
	Total          int      `json:"total"`
	Categories     []string `json:"categories"`
}

type xifanDuanjuContent struct {
	DuanjuVo xifanDuanjuVo `json:"duanjuVo"`
}

type xifanCategoryContent struct {
	CategoryItemVo xifanCategoryItem `json:"categoryItemVo"`
}

type xifanDetailResult struct {
	Source       string             `json:"source"`
	DuanjuID     string             `json:"duanjuId"`
	Title        string             `json:"title"`
	CoverImageURL string           `json:"coverImageUrl"`
	EpisodeList  []xifanEpisode    `json:"episodeList"`
}

type xifanEpisode struct {
	EpisodeID string `json:"episodeId"`
	Index     int    `json:"index"`
	Title     string `json:"title"`
	Unlock    bool   `json:"unlock"`
	PlayURL   string `json:"playUrl"`
}

type xifanAPIResponse struct {
	Ret    int             `json:"ret"`
	Result json.RawMessage `json:"result"`
}

func xifanPlainText(value string) string {
	return strings.TrimSpace(reXifanHTMLTag.ReplaceAllString(value, ""))
}

func xifanHTTPSURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

func (d *Downloader) xifanGet(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("version", "2001001")
	params.Set("androidVersionCode", "28")
	address := xifanSiteBaseURL + path + "?" + params.Encode()
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, xifanUserAgent)
	body, err := d.fetchProviderText(pageContext, address, xifanSiteBaseURL+"/")
	if err != nil {
		return nil, err
	}
	var resp xifanAPIResponse
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		return nil, errors.New("西饭来源响应格式无效")
	}
	if resp.Ret != 0 || len(resp.Result) == 0 {
		return nil, errors.New("西饭来源暂不可用，请稍后重试")
	}
	return resp.Result, nil
}

func xifanExtractContents(result json.RawMessage) []json.RawMessage {
	var portal xifanPortalResult
	if err := json.Unmarshal(result, &portal); err != nil {
		return nil
	}
	var out []json.RawMessage
	for _, element := range portal.Elements {
		out = append(out, element.Contents...)
	}
	return out
}

func (d *Downloader) fetchXifanGenres(ctx context.Context) ([]xifanGenre, error) {
	xifanGenreCache.mu.Lock()
	if time.Now().Before(xifanGenreCache.expiry) && len(xifanGenreCache.items) > 0 {
		items := append([]xifanGenre{}, xifanGenreCache.items...)
		xifanGenreCache.mu.Unlock()
		return items, nil
	}
	xifanGenreCache.mu.Unlock()

	result, err := d.xifanGet(ctx, "/xifan/drama/portalPage", url.Values{"reqType": {"duanjuCategory"}})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var items []xifanGenre
	for _, raw := range xifanExtractContents(result) {
		var content xifanCategoryContent
		if err := json.Unmarshal(raw, &content); err != nil {
			continue
		}
		candidates := []xifanCategoryItem{content.CategoryItemVo}
		if len(content.CategoryItemVo.SubCategories) > 0 {
			candidates = content.CategoryItemVo.SubCategories
		}
		for _, item := range candidates {
			if item.CategoryID <= 0 {
				continue
			}
			label := xifanPlainText(item.OppoCategory)
			if label == "" {
				continue
			}
			slug := fmt.Sprintf("%d-%x", item.CategoryID, label)
			if seen[slug] {
				continue
			}
			seen[slug] = true
			version := item.Version
			if version == 0 {
				version = 1
			}
			items = append(items, xifanGenre{Slug: slug, Label: label, CategoryID: item.CategoryID, Version: version})
		}
	}
	if len(items) == 0 {
		return nil, errors.New("西饭没有返回可用分类，请重试")
	}
	xifanGenreCache.mu.Lock()
	xifanGenreCache.items = items
	xifanGenreCache.expiry = time.Now().Add(xifanGenreTTL)
	xifanGenreCache.mu.Unlock()
	return items, nil
}

func (d *Downloader) fetchXifanCategories(ctx context.Context) ([]nativeCategory, error) {
	items, err := d.fetchXifanGenres(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]nativeCategory, 0, len(items))
	for _, item := range items {
		out = append(out, nativeCategory{ID: item.Slug, Name: item.Label})
	}
	return out, nil
}

func validXifanCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 128 || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	return true
}

func xifanNormalizeList(result json.RawMessage) []Drama {
	seen := map[string]bool{}
	var items []Drama
	for _, raw := range xifanExtractContents(result) {
		var content xifanDuanjuContent
		if err := json.Unmarshal(raw, &content); err != nil {
			continue
		}
		vo := content.DuanjuVo
		if !reXifanID.MatchString(vo.Source) || !reXifanID.MatchString(vo.DuanjuID) {
			continue
		}
		title := xifanPlainText(vo.Title)
		if title == "" {
			continue
		}
		sourceID := vo.Source + ":" + vo.DuanjuID
		if seen[sourceID] {
			continue
		}
		seen[sourceID] = true
		cover := xifanHTTPSURL(vo.CoverImageURL)
		var tags []string
		for _, tag := range vo.Categories {
			tag = xifanPlainText(tag)
			if tag != "" {
				tags = append(tags, tag)
			}
			if len(tags) >= 4 {
				break
			}
		}
		remark := ""
		if vo.Total > 0 {
			remark = fmt.Sprintf("%d集", vo.Total)
		}
		items = append(items, Drama{
			ID:           providerDramaID(sourceXifan, sourceID),
			Source:       sourceXifan,
			SourceID:     sourceID,
			Title:        truncate(title, 512),
			Name:         truncate(title, 512),
			Cover:        cover,
			CoverURL:     cover,
			ChannelName:  "西饭短剧",
			CategoryName: "短剧",
			Remark:       truncate(remark, 64),
			Tags:         tags,
		})
	}
	return items
}

func (d *Downloader) fetchXifanCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 {
		return nil, false, errors.New("西饭短剧目录页码无效")
	}
	if query != "" {
		return d.searchXifan(ctx, page, query)
	}
	if !validXifanCategory(category) {
		return nil, false, errors.New("西饭短剧内容分类无效")
	}
	items, err := d.fetchXifanGenres(ctx)
	if err != nil {
		return nil, false, err
	}
	var selected *xifanGenre
	for i := range items {
		if category == "" && items[i].Label == "都市" {
			selected = &items[i]
			break
		}
		if category != "" && items[i].Slug == category {
			selected = &items[i]
			break
		}
	}
	if selected == nil {
		if category == "" && len(items) > 0 {
			selected = &items[0]
		} else {
			return nil, false, errors.New("西饭题材无效，请重新选择")
		}
	}
	params := url.Values{
		"reqType":        {"aggregationPage"},
		"offset":         {strconv.Itoa((page - 1) * xifanPageSize)},
		"categoryId":     {strconv.Itoa(selected.CategoryID)},
		"categoryNames":  {selected.Label},
		"categoryVersion": {strconv.Itoa(selected.Version)},
	}
	result, err := d.xifanGet(ctx, "/xifan/drama/portalPage", params)
	if err != nil {
		return nil, false, err
	}
	dramas := xifanNormalizeList(result)
	var portal xifanPortalResult
	_ = json.Unmarshal(result, &portal)
	hasMore := portal.HasMore && len(dramas) > 0
	return dramas, hasMore, nil
}

func (d *Downloader) searchXifan(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, false, errors.New("西饭短剧搜索关键词无效")
	}
	result, err := d.xifanGet(ctx, "/xifan/search/getSearchList", url.Values{
		"keyword":   {query},
		"pageIndex": {strconv.Itoa(page)},
	})
	if err != nil {
		return nil, false, err
	}
	dramas := xifanNormalizeList(result)
	var portal xifanPortalResult
	_ = json.Unmarshal(result, &portal)
	hasMore := portal.HasMore && len(dramas) > 0
	return dramas, hasMore, nil
}

func xifanParseSourceID(sourceID string) (source, duanjuID string, err error) {
	parts := strings.Split(strings.TrimSpace(sourceID), ":")
	if len(parts) != 2 || !reXifanID.MatchString(parts[0]) || !reXifanID.MatchString(parts[1]) {
		return "", "", errors.New("西饭剧集编号无效")
	}
	return parts[0], parts[1], nil
}

func (d *Downloader) fetchXifanDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	source, duanjuID, err := xifanParseSourceID(sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	result, err := d.xifanGet(ctx, "/xifan/drama/getDuanjuInfo", url.Values{
		"source":    {source},
		"duanjuId":  {duanjuID},
	})
	if err != nil {
		return Drama{}, nil, err
	}
	var detail xifanDetailResult
	if err := json.Unmarshal(result, &detail); err != nil {
		return Drama{}, nil, errors.New("西饭详情响应格式无效")
	}
	if detail.Source != source || detail.DuanjuID != duanjuID {
		return Drama{}, nil, errors.New("西饭返回的剧集与请求不一致")
	}
	name := xifanPlainText(detail.Title)
	if name == "" {
		name = "未命名短剧"
	}
	cover := xifanHTTPSURL(detail.CoverImageURL)
	drama := Drama{
		ID:          providerDramaID(sourceXifan, sourceID),
		Source:      sourceXifan,
		SourceID:    sourceID,
		Title:       truncate(name, 512),
		Name:        truncate(name, 512),
		Cover:       cover,
		CoverURL:    cover,
		ChannelName: "西饭短剧",
		CategoryName: "短剧",
	}
	var chapters []Chapter
	for _, ep := range detail.EpisodeList {
		if !reXifanID.MatchString(ep.EpisodeID) {
			continue
		}
		order := ep.Index
		if order <= 0 {
			order = len(chapters) + 1
		}
		epTitle := xifanPlainText(ep.Title)
		if epTitle == "" {
			epTitle = fmt.Sprintf("第%d集", order)
		}
		playURL := ""
		if ep.Unlock {
			playURL = xifanHTTPSURL(ep.PlayURL)
		}
		chapters = append(chapters, Chapter{
			ID:             providerChapterID(sourceXifan, sourceID, ep.EpisodeID),
			Source:         sourceXifan,
			Title:          truncate(epTitle, 128),
			CurrentEpisode: rawEpisode(order),
			VideoURL:       playURL,
			PageURL:        ep.EpisodeID,
			Referer:        xifanSiteBaseURL + "/",
		})
	}
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("西饭分集列表为空")
	}
	sortProviderChapters(chapters)
	drama.TotalEpisode = len(chapters)
	drama.EpisodeCount = len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) resolveXifanMedia(ctx context.Context, task Task) (providerMedia, error) {
	_, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid {
		return providerMedia{}, errors.New("西饭短剧播放信息无效")
	}
	chapterID := strings.TrimSpace(task.Chapter.PageURL)
	if chapterID == "" {
		prefix := providerChapterID(sourceXifan, sourceID, "")
		chapterID = strings.TrimPrefix(task.Chapter.ID, prefix)
	}
	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		return providerMedia{}, errors.New("西饭短剧集信息无效，请刷新详情")
	}
	mediaURL := strings.TrimSpace(task.Chapter.VideoURL)
	if mediaURL == "" {
		_, chapters, err := d.fetchXifanDetail(ctx, sourceID)
		if err != nil {
			return providerMedia{}, err
		}
		for _, ch := range chapters {
			if ch.PageURL == chapterID && ch.VideoURL != "" {
				mediaURL = ch.VideoURL
				break
			}
		}
		if mediaURL == "" {
			return providerMedia{}, errors.New("西饭短剧该集当前锁定或暂无可用播放地址")
		}
	}
	if !isProviderHTTPMediaURL(mediaURL) {
		return providerMedia{}, errors.New("西饭短剧播放地址无效")
	}
	media := providerMedia{URL: mediaURL, Referer: xifanSiteBaseURL + "/"}
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return providerMedia{}, errors.New("西饭短剧播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "西饭短剧")
	}
	return media, nil
}