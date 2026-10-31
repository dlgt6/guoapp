package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	qimaoSiteBaseURL = "https://7maoduanju.com"
	qimaoUserAgent   = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	qimaoPageSize    = 48
)

var qimaoCategories = []nativeCategory{
	{ID: "热门短剧", Name: "热门短剧"},
	{ID: "真人短剧", Name: "真人短剧"},
	{ID: "原创短剧", Name: "原创短剧"},
	{ID: "AI 短剧", Name: "AI 短剧"},
	{ID: "AI 漫剧", Name: "AI 漫剧"},
	{ID: "AI 换脸", Name: "AI 换脸"},
	{ID: "AI 魔改", Name: "AI 魔改"},
	{ID: "影视综艺", Name: "影视综艺"},
	{ID: "擦边短剧", Name: "擦边短剧"},
}

var (
	reQimaoCard       = regexp.MustCompile(`(?is)<a class="drama-card-link" href="/drama/([0-9a-f-]{36})/">`)
	reQimaoCardImg    = regexp.MustCompile(`(?is)<img[^>]*src="([^"]+)"[^>]*alt="([^"]*)"`)
	reQimaoCardTitle  = regexp.MustCompile(`(?is)<span class="drama-card-link__title">([^<]*)</span>`)
	reQimaoCardHeat   = regexp.MustCompile(`(?is)<span class="drama-card-link__heat">[^<]*<!--\s*-->([^<]*)</span>`)
	reQimaoCardAccess = regexp.MustCompile(`(?is)<span class="drama-card-link__access"[^>]*>([^<]*)</span>`)
	reQimaoUUID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reQimaoEpisodes   = regexp.MustCompile(`(?s)\\"episodes\\":\[`)
	reQimaoEpID       = regexp.MustCompile(`(?s)\\"id\\":\\"([0-9a-f-]{36})\\"`)
	reQimaoEpIndexNo  = regexp.MustCompile(`(?s)\\"indexNo\\":(\d+)`)
	reQimaoEpLocked   = regexp.MustCompile(`(?s)\\"locked\\":(true|false)`)
	reQimaoEpTitle    = regexp.MustCompile(`(?s)\\"title\\":\\"((?:[^"\\]|\\.)*)\\"`)
	reQimaoEpContent  = regexp.MustCompile(`(?s)\\"seoContentUrl\\":\\"([^"\\]+)\\"`)
)

func validQimaoCategory(category string) bool {
	if category == "" {
		return true
	}
	if len(category) > 128 || strings.ContainsAny(category, "|/\\\x00\r\n") {
		return false
	}
	for _, entry := range qimaoCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

func qimaoUUID(id string) bool {
	return reQimaoUUID.MatchString(strings.TrimSpace(id))
}

func (d *Downloader) fetchQimaoCategories() []nativeCategory {
	return append([]nativeCategory(nil), qimaoCategories...)
}

func (d *Downloader) qimaoGet(ctx context.Context, path string) (string, error) {
	site := d.providerBaseURL(sourceQimao)
	address := path
	if !strings.HasPrefix(address, "http") {
		address = site + path
	}
	pageContext := context.WithValue(ctx, providerTextUserAgentKey{}, qimaoUserAgent)
	return d.fetchProviderText(pageContext, address, site+"/")
}

func qimaoFixURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	} else if strings.HasPrefix(raw, "/") {
		raw = qimaoSiteBaseURL + raw
	}
	parsed, err := url.Parse(raw)
	if err != nil || !validNativeCoverURL(parsed) {
		return ""
	}
	return parsed.String()
}

func parseQimaoCards(body string) []Drama {
	items := make([]Drama, 0, qimaoPageSize)
	seen := map[string]bool{}
	for _, match := range reQimaoCard.FindAllStringSubmatchIndex(body, -1) {
		if len(match) < 4 {
			continue
		}
		id := body[match[2]:match[3]]
		if seen[id] {
			continue
		}
		chunk := body[match[1]:]
		if end := len(chunk); end > 1200 {
			chunk = chunk[:1200]
		}
		title := ""
		cover := ""
		if imgMatch := reQimaoCardImg.FindStringSubmatch(chunk); len(imgMatch) > 2 {
			cover = qimaoFixURL(imgMatch[1])
			title = strings.TrimSpace(imgMatch[2])
		}
		if title == "" {
			if titleMatch := reQimaoCardTitle.FindStringSubmatch(chunk); len(titleMatch) > 1 {
				title = strings.TrimSpace(titleMatch[1])
			}
		}
		if title == "" {
			continue
		}
		remark := ""
		if heatMatch := reQimaoCardHeat.FindStringSubmatch(chunk); len(heatMatch) > 1 {
			remark = strings.TrimSpace(heatMatch[1])
		}
		if remark == "" {
			if accessMatch := reQimaoCardAccess.FindStringSubmatch(chunk); len(accessMatch) > 1 {
				remark = strings.TrimSpace(accessMatch[1])
			}
		}
		seen[id] = true
		items = append(items, Drama{
			ID:           providerDramaID(sourceQimao, id),
			Source:       sourceQimao,
			SourceID:     id,
			Title:        truncate(title, 512),
			Name:         truncate(title, 512),
			Cover:        cover,
			CoverURL:     cover,
			ChannelName:  "七猫短剧",
			CategoryName: "短剧",
			Remark:       truncate(remark, 64),
		})
		if len(items) > 500 {
			break
		}
	}
	return items
}

func (d *Downloader) fetchQimaoCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 {
		return nil, false, errors.New("七猫短剧目录页码无效")
	}
	if query != "" {
		return d.searchQimao(ctx, page, query)
	}
	if !validQimaoCategory(category) {
		return nil, false, errors.New("七猫短剧内容分类无效")
	}
	var path string
	if category == "" {
		path = "/browse/?page=" + strconv.Itoa(page)
	} else {
		path = "/category/" + url.PathEscape(category) + "/?page=" + strconv.Itoa(page)
	}
	body, err := d.qimaoGet(ctx, path)
	if err != nil {
		return nil, false, err
	}
	items := parseQimaoCards(body)
	if len(items) == 0 {
		return []Drama{}, false, nil
	}
	hasMore := len(items) >= qimaoPageSize
	return items, hasMore, nil
}

func (d *Downloader) searchQimao(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 256 {
		return nil, false, errors.New("七猫短剧搜索关键词无效")
	}
	if page > 1 {
		return []Drama{}, false, nil
	}
	body, err := d.qimaoGet(ctx, "/search/?q="+url.QueryEscape(query))
	if err != nil {
		return nil, false, err
	}
	return parseQimaoCards(body), false, nil
}

func qimaoUnescape(s string) string {
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
}

func qimaoDetailField(body, field string) string {
	re := regexp.MustCompile(`(?s)\\"` + field + `\\":\\"((?:[^"\\]|\\.)*)\\"`)
	if m := re.FindStringSubmatch(body); len(m) > 1 {
		return qimaoUnescape(m[1])
	}
	return ""
}

func qimaoDetailTags(body string) []string {
	re := regexp.MustCompile(`(?s)\\"tags\\":\[(.*?)\]`)
	m := re.FindStringSubmatch(body)
	if len(m) < 2 {
		return nil
	}
	var tags []string
	for _, tag := range regexp.MustCompile(`\\"((?:[^"\\]|\\.)*)\\"`).FindAllStringSubmatch(m[1], -1) {
		if len(tag) > 1 {
			tags = append(tags, qimaoUnescape(tag[1]))
		}
	}
	return tags
}

type qimaoEpisode struct {
	ID      string
	IndexNo int
	Title   string
	Locked  bool
	URL     string
}

func qimaoParseEpisodes(body string) []qimaoEpisode {
	loc := reQimaoEpisodes.FindStringIndex(body)
	if loc == nil {
		return nil
	}
	start := loc[1]
	depth := 0
	var episodes []qimaoEpisode
	objStart := -1
	for i := start; i < len(body); i++ {
		ch := body[i]
		if ch == '{' {
			if depth == 0 {
				objStart = i
			}
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 && objStart >= 0 {
				obj := body[objStart : i+1]
				ep := qimaoEpisode{}
				if idMatch := reQimaoEpID.FindStringSubmatch(obj); len(idMatch) > 1 {
					ep.ID = idMatch[1]
				}
				if idxMatch := reQimaoEpIndexNo.FindStringSubmatch(obj); len(idxMatch) > 1 {
					if n, err := strconv.Atoi(idxMatch[1]); err == nil {
						ep.IndexNo = n
					}
				}
				if titleMatch := reQimaoEpTitle.FindStringSubmatch(obj); len(titleMatch) > 1 {
					ep.Title = qimaoUnescape(titleMatch[1])
				}
				if lockedMatch := reQimaoEpLocked.FindStringSubmatch(obj); len(lockedMatch) > 1 {
					ep.Locked = lockedMatch[1] == "true"
				}
				if urlMatch := reQimaoEpContent.FindStringSubmatch(obj); len(urlMatch) > 1 {
					ep.URL = qimaoUnescape(urlMatch[1])
				}
				if ep.ID != "" && ep.IndexNo > 0 {
					episodes = append(episodes, ep)
				}
				objStart = -1
			}
		} else if ch == ']' && depth == 0 {
			break
		}
	}
	return episodes
}

func qimaoGuessEpisodeURL(freeURL string, indexNo int) string {
	if freeURL == "" || indexNo < 1 {
		return ""
	}
	parsed, err := url.Parse(freeURL)
	if err != nil {
		return ""
	}
	path := parsed.Path
	idx := strings.LastIndex(path, "/hls/")
	if idx < 0 {
		return ""
	}
	base := path[:idx+len("/hls/")]
	epTag := fmt.Sprintf("ep%02d", indexNo)
	guessed := base + epTag + "/" + epTag + ".m3u8"
	parsed.Path = guessed
	return parsed.String()
}

func (d *Downloader) fetchQimaoDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !qimaoUUID(sourceID) {
		return Drama{}, nil, errors.New("七猫短剧视频 ID 无效")
	}
	body, err := d.qimaoGet(ctx, "/drama/"+sourceID+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	name := qimaoDetailField(body, "title")
	if name == "" {
		if m := regexp.MustCompile(`(?s)<title[^>]*>《(.*?)》`).FindStringSubmatch(body); len(m) > 1 {
			name = strings.TrimSpace(m[1])
		}
	}
	if name == "" {
		name = sourceID
	}
	cover := qimaoFixURL(qimaoDetailField(body, "coverUrl"))
	description := qimaoDetailField(body, "description")
	tags := qimaoDetailTags(body)
	heat := qimaoDetailField(body, "heat")
	payMode := qimaoDetailField(body, "payMode")
	remark := ""
	if heat != "" {
		remark = heat + " 热度"
	}
	if payMode != "" && payMode != "free" {
		if remark != "" {
			remark += " · "
		}
		remark += "VIP"
	}
	episodes := qimaoParseEpisodes(body)
	if len(episodes) == 0 {
		return Drama{}, nil, errors.New("七猫短剧暂无可播放分集")
	}
	var freeURL string
	for _, ep := range episodes {
		if ep.URL != "" {
			freeURL = ep.URL
			break
		}
	}
	drama := Drama{
		ID:           providerDramaID(sourceQimao, sourceID),
		Source:       sourceQimao,
		SourceID:     sourceID,
		Title:        truncate(name, 512),
		Name:         truncate(name, 512),
		Desc:         truncate(description, 12000),
		Intro:        truncate(description, 12000),
		Cover:        cover,
		CoverURL:     cover,
		ChannelName:  "七猫短剧",
		CategoryName: strings.Join(tags, " "),
		Remark:       truncate(remark, 64),
		Tags:         tags,
	}
	var chapters []Chapter
	for _, ep := range episodes {
		epTitle := ep.Title
		if epTitle == "" {
			epTitle = fmt.Sprintf("第%d集", ep.IndexNo)
		}
		playURL := ep.URL
		if playURL == "" && freeURL != "" {
			playURL = qimaoGuessEpisodeURL(freeURL, ep.IndexNo)
		}
		chapters = append(chapters, Chapter{
			ID:             providerChapterID(sourceQimao, sourceID, ep.ID),
			Source:         sourceQimao,
			Title:          truncate(epTitle, 128),
			CurrentEpisode: rawEpisode(ep.IndexNo),
			VideoURL:       playURL,
			PageURL:        ep.ID,
			Referer:        qimaoSiteBaseURL + "/",
		})
	}
	sortProviderChapters(chapters)
	drama.TotalEpisode = len(chapters)
	drama.EpisodeCount = len(chapters)
	return drama, chapters, nil
}

func (d *Downloader) resolveQimaoMedia(ctx context.Context, task Task) (providerMedia, error) {
	_, sourceID, valid := splitProviderDramaID(task.DramaID)
	if !valid || !qimaoUUID(sourceID) {
		return providerMedia{}, errors.New("七猫短剧播放信息无效")
	}
	chapterID := task.Chapter.PageURL
	if chapterID == "" {
		prefix := providerChapterID(sourceQimao, sourceID, "")
		chapterID = strings.TrimPrefix(task.Chapter.ID, prefix)
	}
	chapterID = strings.TrimSpace(chapterID)
	if chapterID == "" {
		return providerMedia{}, errors.New("七猫短剧集信息无效，请刷新详情")
	}
	mediaURL := strings.TrimSpace(task.Chapter.VideoURL)
	if mediaURL == "" {
		drama, chapters, err := d.fetchQimaoDetail(ctx, sourceID)
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
			_ = drama
			return providerMedia{}, errors.New("七猫短剧该集暂无可用播放地址")
		}
	}
	if !isProviderHTTPMediaURL(mediaURL) {
		return providerMedia{}, errors.New("七猫短剧播放地址无效")
	}
	media := providerMedia{URL: mediaURL, Referer: qimaoSiteBaseURL + "/"}
	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return providerMedia{}, errors.New("七猫短剧播放地址无效")
	}
	if strings.HasSuffix(strings.ToLower(parsed.Path), ".m3u8") {
		return d.prepareWebProviderMedia(ctx, media, "七猫短剧")
	}
	return media, nil
}