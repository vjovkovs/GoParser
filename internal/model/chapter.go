package model

type Chapter struct {
	Title string `json:"title"`
	Code  string `json:"code"`
	URL   string `json:"url"`
	HTML  string `json:"html"`
}

type ParserConfig struct {
	SelectorCandidates     []string
	NextTexts              []string
	MinTextLen             int
	PoliteDelaySec         int
	SceneBreakPatterns     []string
	SceneBreakStyle        string
	ConsecutiveBRThreshold int
}

func DefaultParserConfig() ParserConfig {
	return ParserConfig{
		SelectorCandidates: []string{"" +
			"article .entry-content", ".entry-content", ".post-content", "article", ".chapter-content"},
		NextTexts: []string{"Next", "Next Chapter", "Next Page", ">>", "Â»", "â†’"},
		//MinTextLen:             800,
		MinTextLen:             50,
		PoliteDelaySec:         1,
		SceneBreakPatterns:     []string{"^[-_*]{3,}$", "^\\* *\\* *\\* *$", "^â€”+$", "^â€ $"},
		SceneBreakStyle:        "hr",
		ConsecutiveBRThreshold: 3,
	}
}
