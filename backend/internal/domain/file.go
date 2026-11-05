package domain

type PackageFileInfo struct {
	Path      string `json:"path"`
	Functions int    `json:"functions"`
	Methods   int    `json:"methods"`
	Types     int    `json:"types"`
	IsTest    bool   `json:"is_test"`
	Size      int64  `json:"size,omitempty"`
}

type SourceFile struct {
	ID                int64             `json:"-"`
	AnalysisID        string            `json:"-"`
	Path              string            `json:"path"`
	FunctionsCount    int               `json:"functions"`
	ClassesCount      int               `json:"classes"`
	InDegree          int               `json:"in_degree"`
	OutDegree         int               `json:"out_degree"`
	IsEntrypoint      bool              `json:"is_entrypoint"`
	ImportanceScore   float64           `json:"importance_score"`
	NodeKind          string            `json:"node_kind,omitempty"` // "file" or "package"
	Language          string            `json:"language,omitempty"`  // "python" or "go"
	ModulePath        string            `json:"module_path,omitempty"`
	PackageImportPath string            `json:"package_import_path,omitempty"`
	DirectoryPath     string            `json:"directory_path,omitempty"`
	PackageName       string            `json:"package_name,omitempty"`
	FilesCount        int               `json:"files_count,omitempty"`
	MethodsCount      int               `json:"methods_count,omitempty"`
	TypesCount        int               `json:"types_count,omitempty"`
	Files             []PackageFileInfo `json:"files,omitempty"`
}

func NodeIDForPath(path string) string {
	return "file:" + path
}

func NodeIDForPackage(pkgImportPath string) string {
	return "pkg:" + pkgImportPath
}
