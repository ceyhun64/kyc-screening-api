package screening

// DemoListVersion identifies the list below. Every screening stores it, so we
// can always tell which list a past decision was based on.
//
// NEDEN versiyon: Yaptırım listeleri sürekli güncellenir. Bugün "clear" olan biri
// yarın listeye eklenebilir. Denetimde "o gün hangi listeye göre temiz dedik?"
// sorusunun cevabı bu versiyon bilgisi.
const DemoListVersion = "demo-2026-09"

// DemoList is a small list of FICTIONAL names used for local development and
// tests. A real system would load the list from a screening provider.
//
// NEDEN kurgusal isimler: Gerçek kişilerin isimlerini bir demo projede "yaptırım
// listesinde" gibi göstermek yanlış olur. Gerçek sistemde liste bir sağlayıcıdan
// (ör. resmi yaptırım listeleri veya ticari bir tarama API'si) yüklenir.
// NEDEN Türkçe karakterli bir isim var: Normalizasyonun Türkçe harflerle çalıştığını göstermek için.
var DemoList = []string{
	"Viktor Blackwood",
	"Elena Marsh-Novak",
	"Kemal Yıldırımoğlu",
	"Rashid Al-Fayeem",
	"Ingrid Solberg",
}
