// Package screening checks a person's name against a sanctions list.
//
// Real screening providers do much more (aliases, dates of birth,
// transliteration tables, phonetic matching). This package keeps the core
// idea small: normalize both names the same way, then measure how similar
// they are.
//
// NEDEN ayrı paket: Tarama mantığı müşteri özelliğinden bağımsız. Başka bir yerde
// (ör. şirket taraması, KYB) tekrar kullanılabilir. Bu paket customer paketini
// hiç import etmiyor; bağımlılık tek yönlü (customer → screening).
package screening

import (
	"math"
	"sort"
	"strings"
	"unicode"
)

// Result is the outcome of screening one name.
type Result struct {
	Match       bool
	MatchedName string
	Score       float64 // 0..1, 1 means identical after normalization
	ListVersion string  // which version of the list was used (audit trail)
}

// NEDEN küçük harfle "entry": Paket dışına açık olmasına gerek yok, iç detay.
// NEDEN normalize edilmiş hali saklanıyor: Liste isimleri her taramada yeniden
// normalize edilmesin. Bir kez, Screener oluşturulurken hesaplanıyor.
type entry struct {
	original   string
	normalized string
}

// Screener compares names against an in-memory list.
type Screener struct {
	entries   []entry
	threshold float64
	version   string
}

// New builds a Screener. threshold is the minimum similarity (0..1) that
// counts as a potential match.
// NEDEN eşik (threshold) parametre: Eşik bir iş kararı. Kodu değiştirmeden ayarlanabilsin
// ve testlerde farklı değerler denenebilsin.
func New(names []string, version string, threshold float64) *Screener {
	entries := make([]entry, 0, len(names)) // NEDEN kapasiteyi önceden veriyorum: Kaç eleman olacağı belli; append sırasında dizinin tekrar tekrar büyütülmesini önlüyor.
	for _, n := range names {
		entries = append(entries, entry{original: n, normalized: Normalize(n)})
	}
	return &Screener{entries: entries, threshold: threshold, version: version}
}

// Screen returns the best match for name. Match is true when the best score
// reaches the threshold.
// NEDEN en iyi skoru arıyorum: İlk eşleşmede durmak yerine listedeki en benzer ismi
// buluyorum. Böylece uzman en olası eşleşmeyi görüyor.
func (s *Screener) Screen(name string) Result {
	target := Normalize(name)
	best := Result{ListVersion: s.version}

	for _, e := range s.entries {
		score := similarity(target, e.normalized)
		if score > best.Score {
			best.Score = score
			best.MatchedName = e.original // NEDEN orijinal isim: Uzmana normalize edilmiş "KEMAL YILDIRIMOGLU" değil, listedeki gerçek yazım gösterilsin.
		}
	}

	best.Score = math.Round(best.Score*1000) / 1000 // NEDEN yuvarlama: 0.8888888 yerine 0.889; API'de okunaklı ve kayıtta tutarlı.
	best.Match = best.Score >= s.threshold
	if !best.Match {
		best.MatchedName = "" // NEDEN: Eşik altındaysa bir isim göstermek yanıltıcı olur ("eşleşme var" gibi algılanabilir).
	}
	return best
}

// NEDEN Replacer paket seviyesinde: strings.NewReplacer bir kez oluşturulup her çağrıda
// tekrar kullanılıyor. Birden fazla goroutine'den aynı anda kullanılması da güvenli.
var turkishToASCII = strings.NewReplacer(
	"İ", "I", "ı", "i",
	"Ş", "S", "ş", "s",
	"Ç", "C", "ç", "c",
	"Ğ", "G", "ğ", "g",
	"Ö", "O", "ö", "o",
	"Ü", "U", "ü", "u",
)

// Normalize makes two spellings of the same name comparable:
// Turkish letters become ASCII, everything is upper case, punctuation is
// removed and the name parts are sorted, so "Yılmaz, Ayşe" and
// "AYSE YILMAZ" both become "AYSE YILMAZ".
//
// NEDEN normalizasyon: Aynı kişi farklı sistemlerde farklı yazılabilir: pasaportta
// "YILDIRIMOGLU", listede "Yıldırımoğlu". Normalize etmeden karşılaştırırsak gerçek bir
// eşleşmeyi kaçırırız (false negative). AML'de bu en tehlikeli hata türü.
func Normalize(name string) string {
	// NEDEN önce Türkçe harfleri çeviriyorum, sonra büyük harfe: strings.ToUpper("i") Go'da "I" verir,
	// ama Türkçe kurallarına göre "İ" olmalıdır. Önce ASCII'ye çevirince bu karışıklık ortadan kalkıyor.
	name = strings.ToUpper(turkishToASCII.Replace(name))

	// NEDEN strings.Map: Her karakteri tek geçişte ya dönüştürüyor ya siliyor (-1 döndürünce siliniyor).
	cleaned := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r
		}
		// NEDEN tire ve virgül boşluğa dönüşüyor: "Marsh-Novak" iki ayrı isim parçası sayılsın.
		if unicode.IsSpace(r) || r == '-' || r == ',' || r == '.' || r == '\'' {
			return ' '
		}
		return -1 // drop anything else
	}, name)

	parts := strings.Fields(cleaned)
	// NEDEN isim parçalarını sıralıyorum: Bazı belgelerde soyad önce yazılır ("Yılmaz Ayşe").
	// Sıralayınca yazım sırası fark etmiyor.
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// similarity returns 1 - (edit distance / length of the longer string).
// NEDEN 0 ile 1 arasına oranlıyorum: Ham edit mesafesi isim uzunluğuna bağlı. Kısa isimde
// 2 harf farkı büyük, uzun isimde küçük bir fark. Oran, eşiği tüm isimler için anlamlı kılıyor.
func similarity(a, b string) float64 {
	if a == "" && b == "" {
		return 1 // NEDEN: Aşağıda sıfıra bölme olmasın.
	}
	longest := max(len(a), len(b)) // NEDEN max builtin: Go 1.21'den beri min/max yerleşik fonksiyon.
	return 1 - float64(levenshtein(a, b))/float64(longest)
}

// levenshtein counts the minimum number of single-character edits
// (insert, delete, replace) needed to turn a into b.
// Inputs are ASCII after Normalize, so indexing bytes is safe.
//
// NEDEN Levenshtein: Yazım hatalarını ve küçük farkları yakalıyor ("Blackwood" / "Blakwood" = 1 fark).
// Gerçek sağlayıcılar ek olarak fonetik algoritmalar (ör. Soundex) ve takma adlar kullanır.
// NEDEN sadece iki satır tutuyorum (prev, curr): Klasik çözüm tam bir matris kullanır
// (len(a) x len(b)). Her adımda yalnızca bir önceki satır gerektiği için bellek O(n) oluyor.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j // boş string'den b'nin ilk j harfine: j ekleme
	}

	for i := 1; i <= len(a); i++ {
		curr[0] = i // a'nın ilk i harfinden boş string'e: i silme
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0 // harfler aynıysa değiştirme maliyeti yok
			}
			curr[j] = min(
				prev[j]+1,      // delete
				curr[j-1]+1,    // insert
				prev[j-1]+cost, // replace
			)
		}
		prev, curr = curr, prev // NEDEN swap: Yeni dizi oluşturmadan satırları yer değiştiriyorum.
	}
	return prev[len(b)]
}
