# DDO Projesi — Mehmet İnan

DestekMasa, bir işyerindeki IT destek taleplerini yönetmek için geliştirilen yardım masası uygulamasıdır. Çalışanlar talep açar; destek ekibi talepleri takip eder. Uygulama SLA sürelerini hesaplar, uygun uzmana atama yapar ve varlıkları etkileyen bakım planları için onay sürecini yönetir.

## Kurulum

Docker ve Docker Compose gerekir. Yerel Go veya Node.js kurulumu gerekmez; ilk derlemede internet bağlantısı gerekir.

```bash
git clone https://github.com/minan6805/ddo-case-mehmet-inan.git
cd ddo-case-mehmet-inan
cp .env.example .env
docker compose up --build -d --wait
```

Depo zaten bilgisayarınızdaysa clone/cd adımlarını; mevcut `.env` dosyanız varsa kopyalama adımını atlayın.

- Arayüz: http://localhost:3000
- API sağlık kontrolü: http://localhost:8080/health
- PostgreSQL yalnızca Compose ağı üzerinden erişilir.
- Durdurma: `docker compose down`. Veriler Docker volume içinde korunur.

Arayüz varsayılan olarak teslim portu 3000 üzerinde çalışır. Portlar `.env` üzerinden değiştirilebilir. Sağ üstteki **Tema** menüsünden Sistem, Açık veya Koyu seçilebilir; seçim tarayıcıda hatırlanır. Sistem seçeneği işletim sisteminin görünümünü izler.

## Demo hesapları

Tüm demo hesaplarının başlangıç parolası: **`DemoDestek2026!`**

| Rol | E-posta |
|---|---|
| Çalışan | calisan@destekmasa.local |
| Destek uzmanı | uzman@destekmasa.local |
| Destek uzmanı 2 | uzman2@destekmasa.local |
| Destek uzmanı 3 | uzman3@destekmasa.local |
| Takım lideri | lider@destekmasa.local |
| Varlık yöneticisi | varlik@destekmasa.local |
| Yönetici | admin@destekmasa.local |

Demo kullanıcıları `DEMO_SEED=true` ile oluşturulur. `DEMO_PASSWORD` ilk oluşturma parolasını belirler; mevcut hesapların parolalarını değiştirmez. Diğer yerel ayarlar [.env.example](.env.example) dosyasındadır. Bu hesaplar yerel demo içindir.

Demo modunda giriş ekranı hesapları ve ortak demo parolasını gösterir. Hesap seçimi formu doldurur; giriş ayrıca onaylanır. Görünüm derleme sırasında `DEMO_SEED` ve `DEMO_PASSWORD` ayarlarından hazırlanır; ayarlar değişince `docker compose up --build -d --wait` ile yeniden derleyin. `DEMO_SEED=false` ile derlenen arayüzde bu bölüm ve parola bulunmaz. Demo modu açıkken parola tarayıcıya açıkça gönderilir; yalnız demo hesabı parolası kullanın. Seed mevcut parolaları değiştirmediği için ayar, veritabanındaki demo parolasıyla eşleşmelidir.

## Özellikler

- **Talepler:** Talep oluşturma, kategori/öncelik, durum takibi ve rol/sahiplik kontrolü. Çalışanlar kendi taleplerini, destek ekibi ortak kuyruğu görür.
- **Mesajlar:** Paylaşılan yanıtlar, yalnız destek ekibinin görebildiği iç notlar ve dosya ekleri.
- **SLA:** Hafta içi 08.30–18.00 iş takvimi, tatiller, müşteri yanıtı beklerken duran çözüm süresi, %80 uyarısı ve ihlal/eskalasyon kayıtları. İlk yanıt, talebin işleme alınmasıyla tamamlanır.
- **Otomatik atama:** Beceri, vardiya, izin ve kapasiteye göre uzman seçimi; uygun uzman yoksa kuyrukta bekleme ve sonradan yeniden atama.
- **E-posta işleme:** Simülatörden yeni talep veya mevcut talebe yanıt, ek dosyalar ve aynı Message-ID için tekrar önleme. Yetkili kullanıcılar talepleri birleştirebilir veya mesajları yeni talebe bölebilir.
- **Bilgi bankası:** Başlık/içerik araması ve kategori filtresi; uzman taslakları, lider/yönetici yayını ve arşivleme.
- **Simülatör:** Ayrı Go servisiyle günlük 800 taleplik tekrarlanabilir akış, e-posta tekrarları/yanıtları, vardiyalar, varlık grafı ve canlı ilerleme ekranı. `make simulate` ile çalışır.
- **Raporlar:** Lider/yönetici için tarih ve kategoriye göre talep dağılımları, SLA sonuçları ve güncel uzman iş yükü.
- **Varlık envanteri:** Varlıklar arasındaki bağımlılık grafı ve döngülerde de sonlu çalışan etki analizi.
- **Değişiklik planlama:** Bakım penceresi, risk ve geri dönüş planı; bağımsız lider/yönetici onayı; ortak varlıkları etkileyen çakışan planların engellenmesi ve karar geçmişi.

## Mimari

```text
Tarayıcı → React / Nginx → Go HTTP API → PostgreSQL 16
```

React ve TypeScript arayüzü oluşturur; Vite derler. Rol bazlı ekranlar, etkileşimli formlar ve ortak bileşenler için React; veri alanı uyuşmazlıklarını derlemede yakalamak için TypeScript seçildi. Vite geliştirme ve statik üretim derlemesini sade tutar. Nginx arayüzü sunar ve `/api` isteklerini Go API'ye yönlendirir. API, Go standart HTTP kütüphanesi ve pgx kullanır. Veritabanı migrasyonları başlangıçta uygulanır.

Kimlik doğrulama bcrypt parola özeti ve HttpOnly oturum çereziyle yapılır. Yetkiler sunucuda kontrol edilir. Kritik işlemler ve denetim kayıtları aynı veritabanı transaction'ında tamamlanır.

Teknik kararlar: [DECISIONS.md](DECISIONS.md).

## Testler

Önce uygulamayı başlatın. Make, Python 3.9+ ve IANA saat dilimi verisi (`tzdata`) ile aşağıdaki komutlar çalıştırılabilir; veritabanı testleri ayrı bir Docker ortamı kullanır.

| Komut | Kapsam |
|---|---|
| `make smoke` | Temel servis bağlantıları |
| `make lint` | Go biçimi ve golangci-lint |
| `make verify-openapi` | OpenAPI sözleşmesinin biçim ve referans kontrolü |
| `make test` | Go testleri |
| `make test-integration` | Gerçek PostgreSQL ile entegrasyon testleri |
| `make verify` | Bütün özellikler ve çekirdek görevler |
| `make simulate` | 800 taleplik günlük iş yükü |
| `make simulate-email` | Tekrarlanabilir e-posta örnekleri |
| `make load` | SLA hesaplama ucunda kısa yük testi |

### Özellik bazında kontrol

| Kimlik | Komut | Beklenen davranış |
|---|---|---|
| F-1 | `make verify-tickets` | Talep oluşur; durum akışı çalışır; başka çalışanın kaydı reddedilir. |
| F-2 / T-1 | `make verify-sla` + `make prove-sla` | İş saatleri/tatiller hesaplanır; beklemede çözüm süresi durur; uyarı ve ihlal tekilleşir. |
| F-3 / T-2 | `make verify-assignment` | Uygun uzman seçilir; eşzamanlı istekler kapasiteyi aşmaz; izinli uzmanın işi yeniden değerlendirilir. |
| F-4 | `make verify-knowledge` | Uzman taslağı çalışanlardan saklanır; lider yayını ve arama çalışır. |
| F-5 / F-6 / T-4 | `make verify-t4` | Döngülü graf sonlu hesaplanır; bağımsız CAB onayı ve ortak etki/pencere çakışması denetlenir. |
| F-7 | `make verify-reports` | Rol kontrolü, tarih/kategori filtreleri, gerçek talep toplamları ve boş rapor doğrulanır. |
| T-3 | `make verify-t3` | E-posta tekrarı çoğalmaz; yanıt/ek, birleştirme/bölme ve iç not görünürlüğü korunur. |

PostgreSQL kanıtları ayrıca `make prove-sla`, `make prove-assignment`, `make prove-t3`, `make prove-t4`, `make prove-knowledge`, `make prove-reports` ve `make prove-simulator` ile çalıştırılabilir. Hepsi kendi geçici veritabanını kullanır.

Demo verisini ilk seed durumuna döndürmek için `make reset-demo CONFIRM=yes` kullanılır. **Bu işlem bu Compose projesinin veritabanı volume'ünü siler.** Onaysız `make reset-demo` veri değiştirmeden durur. Sıfırlama sonrası yeniden giriş gerekir; talepler boş, örnek varlıklar ve makaleler hazır olur.

### Tarayıcı kontrolü

Temel elle kontrol: çalışanla talep açın; uzmanla iç not ekleyin; çalışana dönüp notun görünmediğini denetleyin. Bilgi bankasında makale arayın. Lider hesabıyla raporları, simülasyon sayaçlarını, varlık etki görünümünü ve yeni değişiklik formunu açın. CAB'ın ayrıntılı karar ve çakışma yolları otomatik testlerde ayrıca kapsanır.

İsteğe bağlı Playwright betikleri `scripts/browser-*.cjs` içindedir. Playwright 1.50.1 ve Chromium kurulmuş bir ortamda örneğin `COLOR_SCHEME=dark node scripts/browser-smoke.cjs` koyu görünümde iş akışını çalıştırır. Bunlar veri oluşturur; boş ve ayrı bir demo ortamında çalıştırılmalıdır. `BASE_URL`, `DEMO_PASSWORD` ve `BROWSER_ARTIFACT_DIR` değişkenleri desteklenir. `node scripts/browser-theme.cjs` tema seçimi, kalıcılık, sistem değişimi, mobil yerleşim ve kapalı depolama senaryolarını kontrol eder. İsteğe bağlı `AXE_PATH=/path/to/axe.min.js` ile açık/koyu modül ekranlarında kontrast taraması da yapılır; axe-core uygulama paketine eklenmez.

`make verify`, F-1–F-7 özelliklerini, T-1–T-4 çekirdek görevlerini ve simülatörü denetler; herhangi bir kontrol başarısızsa sıfırdan farklı çıkış kodu döndürür. 26 Ekim doğrulamasında 141 PostgreSQL entegrasyon senaryosu, Go race/vet kontrolleri ve arayüz derlemesi başarılıdır. 26 Ekim’de altı otomatik tarayıcı akışı koyu görünümde tekrar geçti; ayrıca açık/koyu tema, kontrast, sistem tercihi ve seçim kalıcılığı denetlendi. Kullanıcıyla temel ekran ve rol/görünürlük kabul kontrolleri de tamamlandı. Yük testinin kapsamı ve ölçümleri: [docs/LOAD-RESULTS.md](docs/LOAD-RESULTS.md).

Boş Docker önbelleği ve boş veritabanıyla ölçülen başlangıç **55,1 saniye**, ardından `make verify` **63,2 saniye** sürdü; 20 kontrolün tamamı PASS verdi. Yöntem ve kapsam: [docs/DELIVERY-CHECKS.md](docs/DELIVERY-CHECKS.md).

GitHub Actions, her gönderimde Go build/lint/test, arayüz derlemesi ve Docker üzerinden genel doğrulamayı çalıştırır. Ayrıntılar: [docs/CI.md](docs/CI.md).

API sözleşmesi: [OpenAPI 3.0.3](backend/internal/httpapi/openapi.json); çalışan API’den `GET /openapi.json` ile de alınabilir. Kullanım ve doğrulama: [docs/API.md](docs/API.md). İstek örnekleri: [requests.http](requests.http).

## İndeks ölçümleri

200.000 sentetik talep ve 1000 çalışanla PostgreSQL 16 üzerinde gerçek sorgular ölçüldü. Yedi sıcak çalıştırmanın medyanları:

| Sorgu | İndeksli | Yalnız ilgili indeks kaldırılınca |
|---|---:|---:|
| Çalışanın ID imleçli talep listesi | 0,173 ms | 0,977 ms |
| Tarih/kategori filtreli rapor özeti | 0,551 ms | 5,064 ms |

İlk planda `Index Scan tickets_requester_id_desc_idx` doğrudan 21 kayıt getirir (30 shared hit); indeks olmadan 180 aday kayıt sıralanır (210 shared hit). İkinci planda `Bitmap Index Scan tickets_created_category_idx` 137 eşleşmeyi bulur (üst düğüm 144 shared hit); bu indeks olmadan alternatif indeks ve kategori filtresi kullanılır (1764 shared hit).

`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` çıktılarındaki ilgili indeks düğümlerinden seçilen alanlar aşağıdadır. Bunlar yeni ölçüm değil, yukarıdaki deneyin kayıtlı planlarından alıntıdır; süre tablosu tüm sorgunun execution time değeridir.

```json
{
  "Node Type": "Index Scan",
  "Index Name": "tickets_requester_id_desc_idx",
  "Actual Startup Time": 0.037,
  "Actual Total Time": 0.096,
  "Actual Rows": 21,
  "Actual Loops": 1,
  "Index Cond": "((requester_id = 42) AND (id < 180000))",
  "Shared Hit Blocks": 27
}
```

```json
{
  "Node Type": "Bitmap Index Scan",
  "Index Name": "tickets_created_category_idx",
  "Actual Startup Time": 0.049,
  "Actual Total Time": 0.05,
  "Actual Rows": 137,
  "Actual Loops": 1,
  "Index Cond": "((created_at >= '2026-09-30 21:00:00+00'::timestamp with time zone) AND (created_at < '2026-10-01 21:00:00+00'::timestamp with time zone) AND (category_code = 'network'::text))",
  "Shared Hit Blocks": 7
}
```

Ölçüm koşulları, tekrar çalıştırma komutu ve sınırlar: [docs/INDEXES.md](docs/INDEXES.md). Tam `EXPLAIN (ANALYZE, BUFFERS)` çıktıları: [docs/INDEX-RESULTS.json](docs/INDEX-RESULTS.json). Sonuçlar sentetik veri/sıcak önbellek içindir; genel üretim performansı taahhüdü değildir.

## Bilinen sınırlar

- E-posta akışı simülatörle çalışır; gerçek SMTP/IMAP bağlantısı yoktur.
- Bakım planlarının uygulanması uygulama dışında yapılır; uygulama planı, onayı ve sonucu kaydeder.
- Takım bazlı destek kuyruğu, parola değiştirme, giriş hızı sınırlaması ve süresi dolmuş oturumların periyodik temizliği henüz yoktur.
- SLA takvimindeki tatiller 2026 içindir; sonraki yıllar ayrıca eklenmelidir. Demo uzmanlarının vardiyaları hafta sonu da açıktır; SLA iş takvimi bundan bağımsızdır.

## Doğrulama Soruları

### 1. En zorlandığım görev ve alternatifler

En çok zorlandığım konu otomatik atamadaki kilit mantığını ve eşzamanlı isteklerin aynı son kapasiteyi nasıl kullanabildiğini anlamaktı. İki isteğin de boş kapasiteyi okuyup sonra atama yapması limiti aşabilir; yalnızca yükü saymak yeterli değildir.

Profil başına kilit ve yeniden deneme daha fazla paralellik sağlayabilirdi, fakat birden çok adayın puanlanmasını ve kilit sırasını karmaşıklaştırır. Bu proje ölçeğinde atama kararlarını tek PostgreSQL transaction kilidinde sıraya koyduk; aday profil satırlarını da kilitleyip yazımdan önce kapasiteyi yeniden kontrol ediyoruz. Talep, atama geçmişi ve audit birlikte tamamlanıyor. Karşılığında yoğun trafikte atamalar birbirini bekleyebilir. Tasarımın ayrıntısı ADR-011 içindedir.

### 2. Bilinen bir sınırlama

Tek global atama kilidi, farklı uzmanlara gidecek işlerin de sırayla değerlendirilmesine neden olur. Çok yoğun eşzamanlı trafikte tutarlılık korunurken bekleme süresi artabilir. Çoklu takım/havuz bazında kilitleme bu sınırı azaltabilecek bir sonraki tasarımdır; mevcut sürümde uygulanmadı. Ayrıca takvim değişiklikleri bütün geçmişi tek transaction içinde yeniden hesaplar; büyük veri hacminde süre sınırına takılabilir ve bu durumda değişiklik geri alınır.

### 3. İki indeks ve ölçüm farkları

`tickets_requester_id_desc_idx`, çalışanın kendi taleplerini `requester_id` ve `id < before` ile azalan ID sırasında listeleme sorgusu içindir. İndeksli medyan 0,173 ms; yalnız bu indeks kaldırıldığında 0,977 ms ölçüldü.

`tickets_created_category_idx`, açılış tarihi ve kategoriyle rapor grubunu seçen sorgu içindir. İndeksli medyan 0,551 ms; yalnız bu indeks kaldırıldığında 5,064 ms ölçüldü. Bunlar 200.000 sentetik talepte, sıcak önbellekte yedi çalıştırmanın medyanlarıdır. Diğer indeksler açıktır; tüm indekslerin kaldırıldığı deney veya üretim kapasitesi garantisi değildir. Plan alıntıları yukarıda, tam sonuçlar [INDEX-RESULTS.json](docs/INDEX-RESULTS.json) içindedir. Tekrar üretme: `make measure-indexes`.

### 4. Yapay zekâ yardımıyla üretilen kodda düzeltilen örnek

Projeyi yapay zekâ desteğiyle geliştirdim. İncelemede düzeltilen somut örnek, PostgreSQL kanıtlarının test ortamını paylaşmasıydı. Önceki betik sabit veya ortam değişkeninden etkilenebilen Compose proje adını kullanıyordu. İki test aynı anda çalışınca biri temizlenirken diğerinin veritabanını kapatabiliyordu.

Önce:

```sh
docker compose -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from integration
# Çıkış temizliği:
docker compose -f compose.test.yaml down --volumes
```

Sonra, her çağrıda aynı benzersiz ad başlangıç ve temizlikte kullanıldı:

```sh
test_project="destekmasa-tests-$(date +%s)-$$"
docker compose -p "$test_project" -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from integration
# Çıkış temizliği:
docker compose -p "$test_project" -f compose.test.yaml down --volumes
```

Böylece uygulamanın `COMPOSE_PROJECT_NAME` ayarı test projesini değiştiremez ve eşzamanlı kanıtlar birbirinin ortamını temizlemez. Uygulama proje adı dışarıdan verilerek iki kanıt birlikte çalıştırıldı; ikisi de geçti ve uygulama çalışmaya devam etti. Gerçek önce/sonra değişikliği: [30d5d19](https://github.com/minan6805/ddo-case-mehmet-inan/commit/30d5d19f51b726789cb762fe63946a5dfdcde0d9). Bu, yalnız derlemenin geçmesinin test altyapısının güvenli olduğunu kanıtlamadığını gösterir.

### 5. SLA motorunun doğruluk kanıtı

`make verify-sla`, çalışan API üzerinden iş takvimi hesabını, ilk kabulü, bekleme/devam ve çözüm akışını dener. 26 Ekim çalıştırmasında ilgili çıktı:

```text
F-2              PASS     Doğrulandı
T-1              PASS     Doğrulandı
```

Kalıcı uyarı/ihlal/eskalasyon, tekilleştirme, takvim değişiminde yeniden hesaplama ve transaction davranışı için `make prove-sla` kullanılır; bu hedef ayrı PostgreSQL ortamında tüm entegrasyon paketlerini çalıştırır. SLA grubunda 11 alt senaryo vardır. Saf hesap modülü ayrıca bağımsız, dakika sayan referansla 140 tarih/bütçe örneğinde karşılaştırılır. Cuma/hafta sonu, tam ve yarım tatil, bekleme/devam, yeniden açılma ve saat dilimi sınırları kapsanır. Preview API'sinin doğru sonucu tek başına kalıcı eskalasyon kanıtı sayılmaz. Genel `make verify` son çalıştırmasında 20 kimlik ve toplam 141 PostgreSQL alt senaryosu başarılıdır.

## Teknik belgeler

- [Teslim doğrulaması](docs/DELIVERY-CHECKS.md)
- [OpenAPI kullanımı](docs/API.md)
- [İndeks ölçümleri](docs/INDEXES.md)
- [Günlük iş yükü simülatörü](docs/SIMULATOR.md)
- [Raporlar](docs/REPORTS.md)
- [Bilgi bankası](docs/KNOWLEDGE.md)
- [SLA kuralları](docs/SLA.md)
- [Otomatik atama](docs/ASSIGNMENT.md)
- [Mesajlar ve iç notlar](docs/COMMENTS.md)
- [E-posta, birleştirme ve bölme](docs/EMAIL.md)
- [Varlık envanteri ve etki grafı](docs/ASSETS.md)
- [Değişiklik planlama ve CAB onayı](docs/CHANGES.md)
