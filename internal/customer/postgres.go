package customer

import (
	"context"
	"database/sql" // NEDEN database/sql: Go'nun standart veritabanı arayüzü. Bağlantı havuzu (connection pool) dahil geliyor.
	"errors"
	"fmt"
)

// PostgresRepository stores customers and screenings in PostgreSQL.
// NEDEN ORM yok, düz SQL: Go'da EF Core benzeri ağır ORM'ler yaygın değil. SQL'i açıkça
// yazınca veritabanına tam olarak ne gittiği görülüyor, performans sürprizi olmuyor.
// NEDEN "implements Repository" yazmıyorum: Go'da interface'ler örtük. Gerekli metodlar
// varsa tip interface'i otomatik olarak sağlıyor.
type PostgresRepository struct {
	db *sql.DB
}

// NewPostgresRepository creates a repository on top of an open database.
func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// date_of_birth::text returns the date as "YYYY-MM-DD".
// NEDEN kolon listesi tek sabit: Aynı kolonlar birçok sorguda kullanılıyor. Tek yerde olunca
// bir kolon eklemek istediğimde sadece burayı ve scanCustomer'ı değiştiriyorum.
// NEDEN SELECT * değil: Kolon sırası ve sayısı açıkça belli olsun; tabloya kolon eklenince kod bozulmasın.
const customerColumns = `id, full_name, date_of_birth::text, country, status, created_at`

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
// NEDEN küçük bir interface: Tek satır (QueryRow) ve çok satır (Query) sonuçları için aynı
// scanCustomer fonksiyonunu kullanabiliyorum. İkisinde de Scan metodu var.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanCustomer(s rowScanner) (Customer, error) {
	var c Customer
	err := s.Scan(&c.ID, &c.FullName, &c.DateOfBirth, &c.Country, &c.Status, &c.CreatedAt)
	return c, err
}

func (r *PostgresRepository) Create(ctx context.Context, in CreateInput) (Customer, error) {
	// NEDEN $1, $2, $3 parametreleri: SQL injection'a karşı. Değerler SQL metnine eklenmiyor,
	// veritabanına ayrı olarak gönderiliyor. Asla string birleştirme ile SQL yazılmamalı.
	// NEDEN RETURNING: ID, status ve created_at'i veritabanı üretiyor (DEFAULT). RETURNING ile
	// tek sorguda hem ekleyip hem oluşan satırı geri alıyorum, ikinci bir SELECT gerekmiyor.
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO customers (full_name, date_of_birth, country)
		VALUES ($1, $2, $3)
		RETURNING `+customerColumns,
		in.FullName, in.DateOfBirth, in.Country,
	)
	c, err := scanCustomer(row)
	if err != nil {
		return Customer{}, fmt.Errorf("insert customer: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (Customer, error) {
	row := r.db.QueryRowContext(ctx, `SELECT `+customerColumns+` FROM customers WHERE id = $1`, id)
	c, err := scanCustomer(row)
	// NEDEN sql.ErrNoRows'u ErrNotFound'a çeviriyorum: Üst katmanlar veritabanı detayını
	// bilmemeli. Yarın başka bir veritabanına geçsem service ve handler değişmez.
	if errors.Is(err, sql.ErrNoRows) {
		return Customer{}, ErrNotFound
	}
	if err != nil {
		return Customer{}, fmt.Errorf("select customer: %w", err)
	}
	return c, nil
}

func (r *PostgresRepository) List(ctx context.Context, limit, offset int) ([]Customer, error) {
	// NEDEN ORDER BY'da id de var: Aynı created_at değerine sahip iki kayıt olursa sıralama
	// kararsız olur ve sayfalar arasında kayıt atlanabilir/tekrarlanabilir. id ile sıra kesinleşiyor.
	rows, err := r.db.QueryContext(ctx, `
		SELECT `+customerColumns+`
		FROM customers
		ORDER BY created_at DESC, id
		LIMIT $1 OFFSET $2`,
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("select customers: %w", err)
	}
	// NEDEN defer rows.Close(): Kapatılmazsa bağlantı havuza geri dönmez; zamanla havuz
	// tükenir ve servis kilitlenir (connection leak).
	defer rows.Close()

	cs := []Customer{} // empty list encodes as [] instead of null — NEDEN: var cs []Customer yazsaydım sonuç yokken JSON'da "null" çıkardı. İstemciler boş dizi bekler.
	for rows.Next() {
		c, err := scanCustomer(rows)
		if err != nil {
			return nil, fmt.Errorf("scan customer: %w", err)
		}
		cs = append(cs, c)
	}
	// NEDEN rows.Err(): Döngü bir hata yüzünden erken biterse (ör. bağlantı koptu) rows.Next()
	// sadece false döner. Gerçek hatayı ancak rows.Err() ile öğreniyoruz. Sık unutulan bir adım.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate customers: %w", err)
	}
	return cs, nil
}

// SaveScreening inserts the screening and updates the customer status in a
// single transaction, so we never have a status without the screening that
// explains it (or the other way around).
//
// NEDEN transaction: İki yazma işlemi var (durumu güncelle + taramayı ekle). Biri başarılı
// olup diğeri başarısız olursa veri tutarsız olur: ör. müşteri "review" durumunda ama bunu
// açıklayan tarama kaydı yok. Transaction ile ya ikisi birden kaydedilir ya hiçbiri.
// Compliance'ta denetim izinin tutarlı olması şart.
func (r *PostgresRepository) SaveScreening(ctx context.Context, s Screening, newStatus Status) (Screening, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return Screening{}, fmt.Errorf("begin tx: %w", err)
	}
	// Rollback does nothing if Commit already succeeded.
	// NEDEN defer Rollback: Aşağıdaki herhangi bir adımda hata olup fonksiyondan çıkarsak
	// transaction otomatik geri alınıyor. Commit başarılıysa Rollback etkisiz kalıyor.
	// Her hata dalında ayrı ayrı Rollback yazmaktan kurtarıyor. Go'da standart kalıp bu.
	defer tx.Rollback() //nolint:errcheck

	// NEDEN önce UPDATE: Müşteri yoksa etkilenen satır 0 olur ve hemen ErrNotFound dönebiliyorum.
	// Ayrıca UPDATE satırı kilitliyor; aynı müşteri için eşzamanlı iki tarama birbirini bekliyor.
	res, err := tx.ExecContext(ctx, `UPDATE customers SET status = $1 WHERE id = $2`, newStatus, s.CustomerID)
	if err != nil {
		return Screening{}, fmt.Errorf("update customer status: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return Screening{}, fmt.Errorf("rows affected: %w", err)
	} else if n == 0 {
		return Screening{}, ErrNotFound
	}

	// NEDEN sql.NullString: matched_name kolonu NULL olabiliyor (eşleşme yoksa). Boş string ""
	// yerine gerçekten NULL yazmak "değer yok" anlamını veritabanında doğru ifade ediyor.
	matched := sql.NullString{String: s.MatchedName, Valid: s.MatchedName != ""}
	err = tx.QueryRowContext(ctx, `
		INSERT INTO screenings (customer_id, result, matched_name, score, list_version)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, created_at`,
		s.CustomerID, s.Result, matched, s.Score, s.ListVersion,
	).Scan(&s.ID, &s.CreatedAt)
	if err != nil {
		return Screening{}, fmt.Errorf("insert screening: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Screening{}, fmt.Errorf("commit: %w", err)
	}
	return s, nil
}

func (r *PostgresRepository) ListScreenings(ctx context.Context, customerID string) ([]Screening, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, customer_id, result, matched_name, score, list_version, created_at
		FROM screenings
		WHERE customer_id = $1
		ORDER BY created_at DESC, id`,
		customerID,
	)
	if err != nil {
		return nil, fmt.Errorf("select screenings: %w", err)
	}
	defer rows.Close()

	list := []Screening{}
	for rows.Next() {
		var s Screening
		var matched sql.NullString // NEDEN: NULL değeri doğrudan string'e okunamaz, hata verir. Önce NullString'e okuyup sonra string'e çeviriyorum.
		if err := rows.Scan(&s.ID, &s.CustomerID, &s.Result, &matched, &s.Score, &s.ListVersion, &s.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan screening: %w", err)
		}
		s.MatchedName = matched.String // NULL ise "" olur.
		list = append(list, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate screenings: %w", err)
	}
	return list, nil
}
