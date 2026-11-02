package domain

type SourceFile struct {
	ID              int64   `json:"-"`
	AnalysisID      string  `json:"-"`
	Path            string  `json:"path"`
	FunctionsCount  int     `json:"functions"`
	ClassesCount    int     `json:"classes"`
	InDegree        int     `json:"in_degree"`
	OutDegree       int     `json:"out_degree"`
	IsEntrypoint    bool    `json:"is_entrypoint"`
	ImportanceScore float64 `json:"importance_score"`
}

func NodeIDForPath(path string) string {
	return "file:" + path
}
