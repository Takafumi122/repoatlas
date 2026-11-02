package domain

type ImportClassification string

const (
	ImportInternal   ImportClassification = "internal"
	ImportExternal   ImportClassification = "external"
	ImportUnresolved ImportClassification = "unresolved"
)

type ImportReference struct {
	ID             int64
	AnalysisID     string
	SourceFileID   int64
	TargetFileID   *int64
	SourcePath     string
	TargetPath     string
	ImportText     string
	ModuleName     string
	LineNumber     int
	Classification ImportClassification
	ResolutionNote *string
}
