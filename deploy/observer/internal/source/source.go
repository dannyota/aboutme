package source

const (
	RepositoryID = "1318874703"
	Dannyota     = "dannyota"
	Dntmakers    = "dntmakers"
	OwnerPattern = Dannyota + "|" + Dntmakers
)

func Owners() []string { return []string{Dannyota, Dntmakers} }

func OwnerID(owner string) string {
	switch owner {
	case Dannyota:
		return "244348267"
	case Dntmakers:
		return "342323513"
	}
	return ""
}

func Valid(owner string) bool { return OwnerID(owner) != "" }

func Repository(owner string) string {
	if !Valid(owner) {
		return ""
	}
	return "https://github.com/" + owner + "/aboutme"
}
