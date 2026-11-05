package domain

type ImportClassification string

const (
	ImportInternal   ImportClassification = "internal"
	ImportExternal   ImportClassification = "external"
	ImportUnresolved ImportClassification = "unresolved"
)

type ImportReference struct {
	ID               int64                `json:"id,omitempty"`
	AnalysisID       string               `json:"analysis_id,omitempty"`
	SourceFileID     int64                `json:"source_file_id,omitempty"`
	TargetFileID     *int64               `json:"target_file_id,omitempty"`
	SourcePath       string               `json:"source_path"`
	TargetPath       string               `json:"target_path"`
	ImportText       string               `json:"import_text"`
	ModuleName       string               `json:"module_name"`
	LineNumber       int                  `json:"line_number"`
	StartLine        int                  `json:"start_line,omitempty"`
	StartColumn      int                  `json:"start_column,omitempty"`
	EndLine          int                  `json:"end_line,omitempty"`
	EndColumn        int                  `json:"end_column,omitempty"`
	ImportKind       string               `json:"import_kind,omitempty"`
	Classification   ImportClassification `json:"classification"`
	ResolutionReason string               `json:"resolution_reason,omitempty"`
	ResolutionNote   *string              `json:"resolution_note,omitempty"`
	SourceFile       string               `json:"source_file,omitempty"`
	SourcePackage    string               `json:"source_package,omitempty"`
	TargetPackage    string               `json:"target_package,omitempty"`
	Alias            string               `json:"alias,omitempty"`
	ColumnNumber     int                  `json:"column_number,omitempty"`
	BuildConstraints string               `json:"build_constraints,omitempty"`
}

type ImportEvidence struct {
	ID               int64  `json:"id"`
	AnalysisID       string `json:"analysis_id"`
	SourcePath       string `json:"source_path"`
	TargetPath       string `json:"target_path,omitempty"`
	ImportStatement  string `json:"import_statement"`
	ModuleName       string `json:"module_name"`
	StartLine        int    `json:"start_line"`
	StartColumn      int    `json:"start_column"`
	EndLine          int    `json:"end_line"`
	EndColumn        int    `json:"end_column"`
	ImportKind       string `json:"import_kind"`
	ResolutionStatus string `json:"resolution_status"`
	ResolutionReason string `json:"resolution_reason"`
	GitHubURL        string `json:"github_url,omitempty"`
}
