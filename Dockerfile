# NEDEN multi-stage build: Derleme için Go araçlarının tamamı gerekiyor (~300 MB), ama çalıştırmak
# için sadece derlenmiş binary yeterli. İlk aşamada derleyip ikinci aşamaya yalnızca binary'yi
# kopyalıyorum. Sonuç: küçük ve daha güvenli bir imaj.

# Build stage: compile a static binary.
FROM golang:1.24-alpine AS build
WORKDIR /src

# Download dependencies first so this layer is cached between builds.
# NEDEN önce sadece go.mod/go.sum: Docker katmanları önbelleğe alır. Kod değişip bağımlılıklar
# değişmediyse, "go mod download" adımı tekrar çalışmaz ve build çok daha hızlı olur.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# NEDEN CGO_ENABLED=0: C kütüphanelerine bağlı olmayan tamamen statik bir binary üretiyor.
# Böylece içinde hiçbir şey olmayan bir imajda (distroless) çalışabiliyor.
# NEDEN -ldflags="-s -w": Hata ayıklama sembollerini çıkarıp binary'yi küçültüyor.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/api ./cmd/api

# Run stage: only the binary, no shell, runs as a non-root user.
# NEDEN distroless: İçinde shell, paket yöneticisi yok. Saldırgan içeri girse bile kullanabileceği
# araç yok, güvenlik açığı taşıyabilecek paket sayısı çok az.
# NEDEN nonroot: Container root kullanıcıyla çalışmıyor; en az yetki prensibi.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
