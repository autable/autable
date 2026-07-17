package trigger

import "embed"

//go:embed docs/*.md
var documentationFS embed.FS

func Documentation() map[string]string {
	docs := map[string]string{}
	for _, language := range []string{"en-US", "zh-CN"} {
		content, err := documentationFS.ReadFile("docs/" + language + ".md")
		if err == nil {
			docs[language] = string(content)
		}
	}
	return docs
}
