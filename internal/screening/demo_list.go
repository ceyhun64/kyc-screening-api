package screening

// DemoListVersion identifies the list below. Every screening stores it, so we
// can always tell which list a past decision was based on.
const DemoListVersion = "demo-2026-09"

// DemoList is a small list of FICTIONAL names used for local development and
// tests. A real system would load the list from a screening provider.
var DemoList = []string{
	"Viktor Blackwood",
	"Elena Marsh-Novak",
	"Kemal Yıldırımoğlu",
	"Rashid Al-Fayeem",
	"Ingrid Solberg",
}
