package v1alpha1

// Default values.
const (
	DefaultArchitecture  = ArchAMD64
	DefaultPublishPath   = "./repos"
	DefaultKeepSnapshots = 3
)

// Default fills in every field the manifest leaves empty. Defaults that
// depend on the distribution, such as its components, come from the
// built-in catalog instead.
func (r *OfflineRepository) Default() {
	s := &r.Spec
	for i := range s.Targets {
		t := &s.Targets[i]
		if t.Name == "" && t.Distro != nil {
			t.Name = t.Distro.Name
		}
		if len(t.Architectures) == 0 {
			t.Architectures = []string{DefaultArchitecture}
		}
	}
	if s.AutoRepos.Enabled == nil {
		s.AutoRepos.Enabled = ptr(true)
	}
	if s.Resolve.Baseline == "" {
		s.Resolve.Baseline = BaselineMinimal
	}
	if s.Resolve.Alternatives == "" {
		s.Resolve.Alternatives = AlternativesFirst
	}
	if s.Updates.Accept == "" {
		s.Updates.Accept = AcceptAll
	}
	if s.Updates.Publish == "" {
		s.Updates.Publish = PublishApproval
	}
	if s.Publish.Path == "" {
		s.Publish.Path = DefaultPublishPath
	}
	if s.Publish.KeepSnapshots == nil {
		s.Publish.KeepSnapshots = ptr(DefaultKeepSnapshots)
	}
	if s.Export.Format == "" {
		s.Export.Format = ExportFormatTarZst
	}
}

func ptr[T any](v T) *T { return &v }
