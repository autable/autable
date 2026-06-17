package nodes

import "embed"

//go:embed docs/*/*.md
var documentationFS embed.FS

func documentation(name string) map[string]string {
	docs := map[string]string{}
	for _, language := range []string{"en-US", "zh-CN"} {
		content, err := documentationFS.ReadFile("docs/" + language + "/" + name + ".md")
		if err == nil {
			docs[language] = string(content)
		}
	}
	return docs
}
