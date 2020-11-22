package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ChimeraCoder/anaconda"
	"github.com/Jeffail/gabs"
	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html"
	"google.golang.org/api/googleapi"
)

var (
	twitterClient *anaconda.TwitterApi
)

func getTwitterUrls(inputURL string) (map[string]string, error) {
	parts := strings.Split(inputURL, ":")
	if len(parts) < 2 {
		return nil, errors.New("Unable to parse Twitter URL")
	}
	return map[string]string{"https:" + parts[1] + ":orig": filenameFromURL(parts[1])}, nil
}

func getTwitterStatusUrls(inputURL string, channelID string) (map[string]string, error) {
	if twitterClient == nil {
		return nil, errors.New("Invalid Twitter API Keys Set")
	}

	matches := RegexpUrlTwitterStatus.FindStringSubmatch(inputURL)
	statusId, err := strconv.ParseInt(matches[4], 10, 64)
	if err != nil {
		return nil, err
	}

	tweet, err := twitterClient.GetTweet(statusId, nil)
	if err != nil {
		return nil, err
	}

	links := make(map[string]string)
	for _, tweetMedia := range tweet.ExtendedEntities.Media {
		if len(tweetMedia.VideoInfo.Variants) > 0 {
			var lastVideoVariant anaconda.Variant
			for _, videoVariant := range tweetMedia.VideoInfo.Variants {
				if videoVariant.Bitrate >= lastVideoVariant.Bitrate {
					lastVideoVariant = videoVariant
				}
			}
			if lastVideoVariant.Url != "" {
				links[lastVideoVariant.Url] = ""
			}
		} else {
			foundUrls := getDownloadLinks(tweetMedia.Media_url_https, channelID)
			for foundUrlKey, foundUrlValue := range foundUrls {
				links[foundUrlKey] = foundUrlValue
			}
		}
	}
	for _, tweetUrl := range tweet.Entities.Urls {
		foundUrls := getDownloadLinks(tweetUrl.Expanded_url, channelID)
		for foundUrlKey, foundUrlValue := range foundUrls {
			links[foundUrlKey] = foundUrlValue
		}
	}

	return links, nil
}

func getInstagramUrls(url string) (map[string]string, error) {
	username, shortcode := getInstagramInfo(url)
	filename := fmt.Sprintf("instagram %s - %s", username, shortcode)
	// if instagram video
	videoUrl := getInstagramVideoUrl(url)
	if videoUrl != "" {
		return map[string]string{videoUrl: filename + filepathExtension(videoUrl)}, nil
	}
	// if instagram album
	albumUrls := getInstagramAlbumUrls(url)
	if len(albumUrls) > 0 {
		links := make(map[string]string)
		for i, albumUrl := range albumUrls {
			links[albumUrl] = filename + " " + strconv.Itoa(i+1) + filepathExtension(albumUrl)
		}
		return links, nil
	}
	// if instagram picture
	afterLastSlash := strings.LastIndex(url, "/")
	mediaUrl := url[:afterLastSlash]
	mediaUrl += strings.Replace(strings.Replace(url[afterLastSlash:], "?", "&", -1), "/", "/media/?size=l", -1)
	return map[string]string{mediaUrl: filename + ".jpg"}, nil
}

func getInstagramInfo(url string) (string, string) {
	resp, err := http.Get(url)

	if err != nil {
		return "N/A", "N/A"
	}

	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)

ParseLoop:
	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			break ParseLoop
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			t := z.Token()
			for _, a := range t.Attr {
				if a.Key == "type" {
					if a.Val == "text/javascript" {
						z.Next()
						content := string(z.Text())
						if strings.Contains(content, "window._sharedData = ") {
							content = strings.Replace(content, "window._sharedData = ", "", 1)
							content = content[:len(content)-1]
							jsonParsed, err := gabs.ParseJSON([]byte(content))
							if err != nil {
								log.Println("Error parsing instagram json:", err)
								continue ParseLoop
							}
							entryChildren, err := jsonParsed.Path("entry_data.PostPage").Children()
							if err != nil {
								log.Println("Unable to find entries children:", err)
								continue ParseLoop
							}
							for _, entryChild := range entryChildren {
								shortcode := entryChild.Path("graphql.shortcode_media.shortcode").Data().(string)
								username := entryChild.Path("graphql.shortcode_media.owner.username").Data().(string)
								return username, shortcode
							}
						}
					}
				}
			}
		}
	}
	return "N/A", "N/A"
}

func getInstagramVideoUrl(url string) string {
	resp, err := http.Get(url)

	if err != nil {
		return ""
	}

	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)

	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			return ""
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			t := z.Token()
			if t.Data == "meta" {
				for _, a := range t.Attr {
					if a.Key == "property" {
						if a.Val == "og:video" || a.Val == "og:video:secure_url" {
							for _, at := range t.Attr {
								if at.Key == "content" {
									return at.Val
								}
							}
						}
					}
				}
			}
		}
	}
}

func getInstagramAlbumUrls(url string) []string {
	var links []string
	resp, err := http.Get(url)

	if err != nil {
		return links
	}

	defer resp.Body.Close()
	z := html.NewTokenizer(resp.Body)

ParseLoop:
	for {
		tt := z.Next()
		switch {
		case tt == html.ErrorToken:
			break ParseLoop
		}
		if tt == html.StartTagToken || tt == html.SelfClosingTagToken {
			t := z.Token()
			for _, a := range t.Attr {
				if a.Key == "type" {
					if a.Val == "text/javascript" {
						z.Next()
						content := string(z.Text())
						if strings.Contains(content, "window._sharedData = ") {
							content = strings.Replace(content, "window._sharedData = ", "", 1)
							content = content[:len(content)-1]
							jsonParsed, err := gabs.ParseJSON([]byte(content))
							if err != nil {
								log.Println("Error parsing instagram json: ", err)
								continue ParseLoop
							}
							entryChildren, err := jsonParsed.Path("entry_data.PostPage").Children()
							if err != nil {
								log.Println("Unable to find entries children: ", err)
								continue ParseLoop
							}
							for _, entryChild := range entryChildren {
								albumChildren, err := entryChild.Path("graphql.shortcode_media.edge_sidecar_to_children.edges").Children()
								if err != nil {
									continue ParseLoop
								}
								for _, albumChild := range albumChildren {
									link, ok := albumChild.Path("node.display_url").Data().(string)
									if ok {
										links = append(links, link)
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if len(links) > 0 {
		log.Printf("Found instagram album with %d images (url: %s)\n", len(links), url)
	}

	return links
}

func getFacebookVideoUrls(url string) (map[string]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyContent, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	foundHD := string(RegexpFacebookVideoHD.Find(bodyContent))
	if len(foundHD) > 0 {
		foundHD_clean := foundHD[8 : len(foundHD)-1]
		foundHD_clean = html.UnescapeString(foundHD_clean)
		return map[string]string{foundHD_clean: ""}, nil
	}

	foundSD := string(RegexpFacebookVideoSD.Find(bodyContent))
	if len(foundSD) > 0 {
		foundSD_clean := foundSD[8 : len(foundSD)-1]
		foundSD_clean = html.UnescapeString(foundSD_clean)
		return map[string]string{foundSD_clean: ""}, nil
	}

	return nil, errors.New("Unable to find source url for Facebook video")
}

func getImgurSingleUrls(url string) (map[string]string, error) {
	url = regexp.MustCompile(`(r\/[^\/]+\/)`).ReplaceAllString(url, "") // remove subreddit url
	url = strings.Replace(url, "imgur.com/", "imgur.com/download/", -1)
	url = strings.Replace(url, ".gifv", "", -1)
	return map[string]string{url: ""}, nil
}

type ImgurAlbumObject struct {
	Data []struct {
		Link string
	}
}

func getImgurAlbumUrls(url string) (map[string]string, error) {
	url = regexp.MustCompile(`(#[A-Za-z0-9]+)?$`).ReplaceAllString(url, "") // remove anchor
	afterLastSlash := strings.LastIndex(url, "/")
	albumId := url[afterLastSlash+1:]
	headers := make(map[string]string)
	headers["Authorization"] = "Client-ID " + IMGUR_CLIENT_ID
	imgurAlbumObject := new(ImgurAlbumObject)
	getJSONwithHeaders("https://api.imgur.com/3/album/"+albumId+"/images", imgurAlbumObject, headers)
	links := make(map[string]string)
	for _, v := range imgurAlbumObject.Data {
		links[v.Link] = ""
	}
	if len(links) <= 0 {
		return getImgurSingleUrls(url)
	}
	log.Printf("Found imgur album with %d images (url: %s)\n", len(links), url)
	return links, nil
}

type StreamableObject struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Files  struct {
		Mp4 struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"mp4"`
		Mp4Mobile struct {
			URL    string `json:"url"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"mp4-mobile"`
	} `json:"files"`
	URL          string      `json:"url"`
	ThumbnailURL string      `json:"thumbnail_url"`
	Message      interface{} `json:"message"`
}

func getStreamableUrls(url string) (map[string]string, error) {
	matches := RegexpUrlStreamable.FindStringSubmatch(url)
	shortcode := matches[3]
	if shortcode == "" {
		return nil, errors.New("Unable to get shortcode from URL")
	}
	reqUrl := fmt.Sprintf("https://api.streamable.com/videos/%s", shortcode)
	streamable := new(StreamableObject)
	getJSON(reqUrl, streamable)
	if streamable.Status != 2 || streamable.Files.Mp4.URL == "" {
		return nil, errors.New("Streamable object has no download candidate")
	}
	link := streamable.Files.Mp4.URL
	if !strings.HasPrefix(link, "http") {
		link = "https:" + link
	}
	links := make(map[string]string)
	links[link] = ""
	return links, nil
}

type GfycatObject struct {
	GfyItem struct {
		Mp4URL string `json:"mp4Url"`
	} `json:"gfyItem"`
}

func getGfycatUrls(url string) (map[string]string, error) {
	parts := strings.Split(url, "/")
	if len(parts) < 3 {
		return nil, errors.New("Unable to parse Gfycat URL")
	} else {
		gfycatId := parts[len(parts)-1]
		gfycatObject := new(GfycatObject)
		getJSON("https://api.gfycat.com/v1/gfycats/"+gfycatId, gfycatObject)
		gfycatUrl := gfycatObject.GfyItem.Mp4URL
		if url == "" {
			return nil, errors.New("Failed to read response from Gfycat")
		}
		return map[string]string{gfycatUrl: ""}, nil
	}
}

type FlickrPhotoSizeObject struct {
	Label  string `json:"label"`
	Width  int    `json:"width,int,string"`
	Height int    `json:"height,int,string"`
	Source string `json:"source"`
	URL    string `json:"url"`
	Media  string `json:"media"`
}

type FlickrPhotoObject struct {
	Sizes struct {
		Canblog     int                     `json:"canblog"`
		Canprint    int                     `json:"canprint"`
		Candownload int                     `json:"candownload"`
		Size        []FlickrPhotoSizeObject `json:"size"`
	} `json:"sizes"`
	Stat string `json:"stat"`
}

func getFlickrUrlFromPhotoId(photoId string) string {
	reqUrl := fmt.Sprintf("https://www.flickr.com/services/rest/?format=json&nojsoncallback=1&method=%s&api_key=%s&photo_id=%s",
		"flickr.photos.getSizes", config.Credentials.FlickrApiKey, photoId)
	flickrPhoto := new(FlickrPhotoObject)
	getJSON(reqUrl, flickrPhoto)
	var bestSize FlickrPhotoSizeObject
	for _, size := range flickrPhoto.Sizes.Size {
		if bestSize.Label == "" {
			bestSize = size
		} else {
			if size.Width > bestSize.Width || size.Height > bestSize.Height {
				bestSize = size
			}
		}
	}
	return bestSize.Source
}

func getFlickrPhotoUrls(url string) (map[string]string, error) {
	if config.Credentials.FlickrApiKey == "" {
		return nil, errors.New("Invalid Flickr API Key Set")
	}
	matches := RegexpUrlFlickrPhoto.FindStringSubmatch(url)
	photoId := matches[5]
	if photoId == "" {
		return nil, errors.New("Unable to get Photo ID from URL")
	}
	return map[string]string{getFlickrUrlFromPhotoId(photoId): ""}, nil
}

type FlickrAlbumObject struct {
	Photoset struct {
		ID        string `json:"id"`
		Primary   string `json:"primary"`
		Owner     string `json:"owner"`
		Ownername string `json:"ownername"`
		Photo     []struct {
			ID        string `json:"id"`
			Secret    string `json:"secret"`
			Server    string `json:"server"`
			Farm      int    `json:"farm"`
			Title     string `json:"title"`
			Isprimary string `json:"isprimary"`
			Ispublic  int    `json:"ispublic"`
			Isfriend  int    `json:"isfriend"`
			Isfamily  int    `json:"isfamily"`
		} `json:"photo"`
		Page    int    `json:"page"`
		PerPage int    `json:"per_page"`
		Perpage int    `json:"perpage"`
		Pages   int    `json:"pages"`
		Total   string `json:"total"`
		Title   string `json:"title"`
	} `json:"photoset"`
	Stat string `json:"stat"`
}

func getFlickrAlbumUrls(url string) (map[string]string, error) {
	if config.Credentials.FlickrApiKey == "" {
		return nil, errors.New("Invalid Flickr API Key Set")
	}
	matches := RegexpUrlFlickrAlbum.FindStringSubmatch(url)
	if len(matches) < 10 || matches[9] == "" {
		return nil, errors.New("Unable to find Flickr Album ID in URL")
	}
	albumId := matches[9]
	if albumId == "" {
		return nil, errors.New("Unable to get Album ID from URL")
	}
	reqUrl := fmt.Sprintf("https://www.flickr.com/services/rest/?format=json&nojsoncallback=1&method=%s&api_key=%s&photoset_id=%s&per_page=500",
		"flickr.photosets.getPhotos", config.Credentials.FlickrApiKey, albumId)
	flickrAlbum := new(FlickrAlbumObject)
	getJSON(reqUrl, flickrAlbum)
	links := make(map[string]string)
	for _, photo := range flickrAlbum.Photoset.Photo {
		links[getFlickrUrlFromPhotoId(photo.ID)] = ""
	}
	return links, nil
}

func getFlickrAlbumShortUrls(url string) (map[string]string, error) {
	result, err := http.Get(url)
	if err != nil {
		return nil, errors.New("Error getting long URL from shortened Flickr Album URL: " + err.Error())
	}
	if RegexpUrlFlickrAlbum.MatchString(result.Request.URL.String()) {
		return getFlickrAlbumUrls(result.Request.URL.String())
	}
	return nil, errors.New("Encountered invalid URL while trying to get long URL from short Flickr Album URL")
}

func getGoogleDriveUrls(url string) (map[string]string, error) {
	parts := strings.Split(url, "/")
	if len(parts) != 7 {
		return nil, errors.New("unable to parse google drive url")
	}
	fileId := parts[len(parts)-2]
	return map[string]string{"https://drive.google.com/uc?export=download&id=" + fileId: ""}, nil
}

func getGoogleDriveFolderUrls(url string) (map[string]string, error) {
	matches := RegexpUrlGoogleDriveFolder.FindStringSubmatch(url)
	if len(matches) < 4 || matches[3] == "" {
		return nil, errors.New("unable to find google drive folder ID in link")
	}
	if DriveService.BasePath == "" {
		return nil, errors.New("please set up google credentials")
	}
	googleDriveFolderID := matches[3]

	links := make(map[string]string)

	driveQuery := fmt.Sprintf("\"%s\" in parents", googleDriveFolderID)
	driveFields := "nextPageToken, files(id)"
	result, err := DriveService.Files.List().Q(driveQuery).Fields(googleapi.Field(driveFields)).PageSize(1000).Do()
	if err != nil {
		log.Println("driveQuery:", driveQuery)
		log.Println("driveFields:", driveFields)
		log.Println("err:", err)
		return nil, err
	}
	for _, file := range result.Files {
		fileUrl := "https://drive.google.com/uc?export=download&id=" + file.Id
		links[fileUrl] = ""
	}

	for {
		if result.NextPageToken == "" {
			break
		}
		result, err = DriveService.Files.List().Q(driveQuery).Fields(googleapi.Field(driveFields)).PageSize(1000).PageToken(result.NextPageToken).Do()
		if err != nil {
			return nil, err
		}
		for _, file := range result.Files {
			links[file.Id] = ""
		}
	}
	return links, nil
}

// getTistoryUrls downloads tistory URLs
// http://t1.daumcdn.net/cfile/tistory/[…] => http://t1.daumcdn.net/cfile/tistory/[…]
// http://t1.daumcdn.net/cfile/tistory/[…]?original => as is
func getTistoryUrls(link string) (map[string]string, error) {
	if !strings.HasSuffix(link, "?original") {
		link += "?original"
	}
	return map[string]string{link: ""}, nil
}

func getLegacyTistoryUrls(link string) (map[string]string, error) {
	link = strings.Replace(link, "/image/", "/original/", -1)
	return map[string]string{link: ""}, nil
}

func getTistoryWithCDNUrls(urlI string) (map[string]string, error) {
	parameters, _ := url.ParseQuery(urlI)
	if val, ok := parameters["fname"]; ok {
		if len(val) > 0 {
			if RegexpUrlTistoryLegacy.MatchString(val[0]) {
				return getLegacyTistoryUrls(val[0])
			}
		}
	}
	return nil, nil
}

func getPossibleTistorySiteUrls(url string) (map[string]string, error) {
	client := new(http.Client)
	request, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept-Encoding", "identity")
	request.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/65.0.3325.181 Safari/537.36")
	respHead, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	contentType := ""
	for headerKey, headerValue := range respHead.Header {
		if headerKey == "Content-Type" {
			contentType = headerValue[0]
		}
	}
	if !strings.Contains(contentType, "text/html") {
		return nil, nil
	}

	request, err = http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Accept-Encoding", "identity")
	request.Header.Add("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/65.0.3325.181 Safari/537.36")
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}

	doc, err := goquery.NewDocumentFromResponse(resp)
	if err != nil {
		return nil, err
	}

	var links = make(map[string]string)

	doc.Find(".article img, #content img, div[role=main] img, .section_blogview img").Each(func(i int, s *goquery.Selection) {
		foundUrl, exists := s.Attr("src")
		if exists {
			isTistoryCdnUrl := RegexpUrlTistoryLegacyWithCDN.MatchString(foundUrl)
			isTistoryUrl := RegexpUrlTistoryLegacy.MatchString(foundUrl)
			if isTistoryCdnUrl == true {
				finalTistoryUrls, _ := getTistoryWithCDNUrls(foundUrl)
				if len(finalTistoryUrls) > 0 {
					for finalTistoryUrl := range finalTistoryUrls {
						foundFilename := s.AttrOr("filename", "")
						links[finalTistoryUrl] = foundFilename
					}
				}
			} else if isTistoryUrl == true {
				finalTistoryUrls, _ := getLegacyTistoryUrls(foundUrl)
				if len(finalTistoryUrls) > 0 {
					for finalTistoryUrl := range finalTistoryUrls {
						foundFilename := s.AttrOr("filename", "")
						links[finalTistoryUrl] = foundFilename
					}
				}
			}
		}
	})

	if len(links) > 0 {
		log.Printf("[%s] Found tistory album with %d images (url: %s)\n", time.Now().Format(time.Stamp), len(links), url)
	}
	return links, nil
}
