# Teslim doğrulaması — 26 Ekim 2026

## Temiz başlangıç ve genel doğrulama

Uygulamanın kullandığı Docker daemon'ından ayrı bir Docker 27.5.1 daemon'ı oluşturuldu. Test öncesinde bu daemon'da imaj sayısı **0**, derleme önbelleği ve uygulama veritabanı boştu. Compose 2.33.0 kullanıldı. Ana uygulamanın verileri ve Docker önbelleği silinmedi.

| Kontrol | Sonuç |
|---|---:|
| `docker compose up --build -d --wait` | **55,091 saniye**, çıkış kodu 0 |
| Ardından `make verify` | **63,166 saniye**, çıkış kodu 0 |
| Genel doğrulama kimlikleri | **20/20 PASS** |
| Gerçek PostgreSQL entegrasyon senaryoları | **141 PASS** |

Başlangıç süresine uygulama imajlarının çekilmesi, Go/npm bağımlılıkları, derleme, boş veritabanının migrasyon/seed işlemleri ve üç servisin healthy olması dahildir. Testi yürüten ayrı Docker daemon'ının hazırlanması bu süreden önce yapılmıştır; normal kurulumda Docker'ın zaten kurulu olması gibi değerlendirilmiştir. `make verify` ölçümünde test ve simülatör imajları da bu daemon'da ilk kez derlenmiştir.

Bu çalıştırma başlangıç için 5 dakika, genel doğrulama için 15 dakika sınırlarının altındadır. İnternet bağlantısı, kayıt sunucusu yanıtı ve donanım farklı makinelerde süreyi etkiler. Tek ölçüm bütün makineler için süre garantisi değildir.

Makinece okunabilir süreler ve her kontrolün sonucu: [DELIVERY-RESULTS.json](DELIVERY-RESULTS.json). İndeks ölçümleri farklı bir sentetik veri deneyi olarak [INDEXES.md](INDEXES.md) içindedir.

## Aynı kontrolün tekrarı

Normal kullanım için README'deki kurulum ve `make verify` yeterlidir. Aşağıdaki komutlar ayrıca bütünüyle boş Docker önbelleğiyle denemek isteyenler içindir. Yalnız adlandırılan geçici test konteynerini oluşturur/siler. Bu yöntem yerel bir test için ayrı, privileged Docker-in-Docker daemon'ı kullanır; uygulamanın çalışma yöntemi değildir. Ana Docker soketi bu konteynere bağlanmaz.

Repo kökünde, `.env` hazırlanmışken:

```bash
docker run --privileged --detach --name destekmasa-cold-proof \
  --env DOCKER_TLS_CERTDIR= \
  --mount "type=bind,source=$(pwd),target=/workspace,readonly" \
  --workdir /workspace docker:27.5-dind

# Daemon hazır olunca çalışır; ilk anda bağlantı hatası olursa tekrar deneyin.
docker exec destekmasa-cold-proof docker info --format '{{.ServerVersion}}; images={{.Images}}'

# Make/Python yalnız doğrulama araçlarıdır. tzdata, Python zoneinfo için gerekir.
docker exec destekmasa-cold-proof apk add --no-cache make python3 tzdata

time docker exec destekmasa-cold-proof docker compose up --build -d --wait
time docker exec destekmasa-cold-proof make verify

# Yalnız geçici test ortamını temizler.
docker exec destekmasa-cold-proof docker compose down --volumes
docker rm --force --volumes destekmasa-cold-proof
```

## Diğer kontroller

- Go biçimi ve golangci-lint: hatasız. PostgreSQL testleri race detector ile çalıştırılır.
- Frontend TypeScript/Vite derlemesi: başarılı.
- OpenAPI 3.0.3: bağımsız şema doğrulaması ve Go router yol eşleşme testleri başarılı. [API.md](API.md).
- `requests.http`, sözleşmedeki 51 işlemin her biri için örnek içerir.
- Teslim portu: frontend 3000, API 8080; PostgreSQL host'a açılmaz. Örnek ayarlar, doğrulama betikleri ve belgeler aynı portları kullanır.
- GitHub Actions: Go, frontend ve Docker genel doğrulaması çalışır. [CI.md](CI.md).

## Elle kabul kontrolünün kapsamı

26 Ekim kullanıcı kontrolünde çalışan girişi ve talep oluşturma, uzman iç notu ekleme, iç notun çalışandan saklanması, bilgi bankasında arama/okuma, lider raporları, simülasyon sayaçları, varlık etki ekranı ve yeni değişiklik formu denendi. Bu, CAB'ın bütün karar yollarının elle yürütüldüğü anlamına gelmez; eşzamanlı onay, çakışma, graf değişimi ve transaction geri alma senaryoları PostgreSQL/browser testleriyle kapsanır.
