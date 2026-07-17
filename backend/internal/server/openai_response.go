package server

import "strings"

type responsesAPIResponse struct {
	OutputText string                   `json:"output_text"`
	Output     []responsesAPIOutputItem `json:"output"`
}

type responsesAPIOutputItem struct {
	Type    string                       `json:"type"`
	Content []responsesAPIContentItem    `json:"content"`
	Action  *responsesAPIWebSearchAction `json:"action"`
}

type responsesAPIContentItem struct {
	Type        string                   `json:"type"`
	Text        string                   `json:"text"`
	Annotations []responsesAPIAnnotation `json:"annotations"`
}

type responsesAPIAnnotation struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

type responsesAPIWebSearchAction struct {
	Sources []responsesAPIWebSource `json:"sources"`
}

type responsesAPIWebSource struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Title string `json:"title"`
}

func responseOutputText(response responsesAPIResponse) string {
	if text := strings.TrimSpace(response.OutputText); text != "" {
		return text
	}

	for _, output := range response.Output {
		for _, content := range output.Content {
			if content.Type != "output_text" && content.Type != "text" {
				continue
			}
			if text := strings.TrimSpace(content.Text); text != "" {
				return text
			}
		}
	}
	return ""
}

func responseSourceURLs(response responsesAPIResponse) []string {
	urls := make([]string, 0)
	seen := make(map[string]struct{})
	appendURL := func(rawURL string) {
		rawURL = strings.TrimSpace(rawURL)
		if rawURL == "" {
			return
		}
		if _, ok := seen[rawURL]; ok {
			return
		}
		seen[rawURL] = struct{}{}
		urls = append(urls, rawURL)
	}

	for _, output := range response.Output {
		for _, content := range output.Content {
			for _, annotation := range content.Annotations {
				if annotation.Type == "url_citation" {
					appendURL(annotation.URL)
				}
			}
		}
		if output.Action != nil {
			for _, source := range output.Action.Sources {
				appendURL(source.URL)
			}
		}
	}
	return urls
}
