-- NEDEN uuid (otomatik artan sayı değil): ID'ler tahmin edilemiyor. /customers/1, /customers/2
-- diye başkalarının kayıtları gezilemiyor. Ayrıca farklı sistemler arasında veri taşırken çakışmıyor.
-- NEDEN gen_random_uuid(): PostgreSQL 13'ten beri eklenti gerekmeden UUID üretiyor; ID'yi veritabanı üretiyor.
CREATE TABLE customers (
    id            uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name     text        NOT NULL,
    -- NEDEN date tipi: Doğum tarihinde saat yok. Doğru tipi kullanınca geçersiz tarih veritabanına giremez.
    date_of_birth date        NOT NULL,
    -- NEDEN char(2): Ülke kodu her zaman 2 harf (ISO 3166-1 alpha-2, ör. TR, MT).
    country       char(2)     NOT NULL,
    -- NEDEN CHECK kısıtı: Uygulamada zaten doğruluyorum ama veritabanı son savunma hattı.
    -- Bir hata veya elle yapılan bir UPDATE yüzünden geçersiz bir durum yazılamaz.
    status        text        NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'clear', 'review')),
    -- NEDEN timestamptz: Saat dilimiyle birlikte saklanıyor. Malta, Türkiye, UTC karışıklığı olmuyor.
    created_at    timestamptz NOT NULL DEFAULT now()
);

-- NEDEN bu index: Liste endpoint'i "en yeniden eskiye" sıralıyor. Index olmadan her
-- listelemede tüm tablo sıralanırdı.
CREATE INDEX customers_created_at_idx ON customers (created_at DESC);

-- Screenings are append-only: every check is kept as an audit record.
-- NEDEN ayrı tablo: Bir müşterinin birden fazla taraması olabilir (bire-çok ilişki).
-- Sonucu müşteri tablosunda tek kolonda tutsaydım geçmiş kaybolurdu.
CREATE TABLE screenings (
    id           uuid             PRIMARY KEY DEFAULT gen_random_uuid(),
    -- NEDEN REFERENCES (foreign key): Var olmayan bir müşteriye ait tarama kaydı oluşamaz.
    customer_id  uuid             NOT NULL REFERENCES customers (id),
    result       text             NOT NULL CHECK (result IN ('clear', 'potential_match')),
    -- NEDEN NULL olabilir: Eşleşme yoksa eşleşen isim de yok.
    matched_name text,
    score        double precision NOT NULL,
    list_version text             NOT NULL,
    created_at   timestamptz      NOT NULL DEFAULT now()
);

-- NEDEN (customer_id, created_at DESC) birleşik index: "Bir müşterinin tarama geçmişi, en yeniden
-- eskiye" sorgusu tam olarak bu iki kolonu kullanıyor. Tek index hem filtreyi hem sıralamayı karşılıyor.
CREATE INDEX screenings_customer_id_idx ON screenings (customer_id, created_at DESC);
