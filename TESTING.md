# Doğrulama rehberi — 26 Ekim 2026

Bu rehber, demo hesaplarını, özellik bazında test komutlarını, beklenen sonuçları ve elle kabul senaryolarını içerir.

## Kurulum ve ana doğrulama

Docker/Compose, Make ve host üzerinde Python 3 gerekir. Python zoneinfo için Europe/Istanbul saat dilimi verisi bulunmalıdır. Go ve arayüz derlemeleri Docker içinde yapılabilir.

```bash
# Yalnız ilk kurulumda; mevcut .env dosyasını ezmeyin.
cp .env.example .env
docker compose up --build -d --wait
make verify
```

Arayüz http://localhost:3000, API http://localhost:8080. PostgreSQL host portuna açılmaz. Migrasyon ve başlangıç verileri otomatik uygulanır.

`make verify`, çalışan ve seed edilmiş sistemde aşağıdaki 20 kimliği kontrol eder. Her satır PASS/FAIL olur; başarısızlık sıfırdan farklı çıkış kodu üretir. Uzun yük testi bu komuta dahil değildir. 26 Ekim son çalıştırmasında 20/20 PASS; PostgreSQL kanıtlarında 141 alt senaryo PASS.

Canlı API kontrolleri demo veritabanına doğrulama kayıtları ekler; bazı ayarları aynı değerle kaydederek version/audit artırır. İzole PostgreSQL kanıtları ayrı geçici Compose projesinde çalışır ve kendi ortamını temizler. Simülatör kanıtı canlı sistemde küçük bir çalışma bırakabilir. Bu nedenle kabul testi sırasında aynı anda doğrulama/simülasyon çalıştırmayın.

## Demo hesapları

Ortak başlangıç parolası: **DemoDestek2026!**. İlk seed sırasında DEMO_PASSWORD değiştirildiyse o parola kullanılır. Seed, mevcut parolayı yeniden yazmaz.

| Rol | E-posta |
| --- | --- |
| Çalışan | calisan@destekmasa.local |
| Uzman | uzman@destekmasa.local |
| Uzman 2 | uzman2@destekmasa.local |
| Uzman 3 | uzman3@destekmasa.local |
| Takım lideri | lider@destekmasa.local |
| Varlık yöneticisi | varlik@destekmasa.local |
| Yönetici | admin@destekmasa.local |

## Kimlik bazında kontrol

Bu tablodaki PASS, 26 Ekim doğrulamasına aittir; başka bir ortamda komut yeniden çalıştırılmalıdır.

| Kimlik | Komut / nasıl denenir | Beklenen sonuç | Son durum |
| --- | --- | --- | --- |
| HEALTH | `make smoke` | API /health, DB bağlantısıyla 200 verir. | PASS |
| WEB-HEALTH | `make smoke` | Nginx /api/health üzerinden aynı sağlık sonucu alınır. | PASS |
| WEB | `make smoke` | Arayüz HTML dosyası erişilir; tek başına JavaScript çalışmasını kanıtlamaz. | PASS |
| AUTH | `make verify-auth` | Demo girişleri ve oturum çalışır; yanlış parola/iptal oturum 401. | PASS |
| RBAC | `make verify-auth` | Yönetici ucunda admin 200, diğer roller 403; yabancı Origin reddedilir. Diğer modül yetkileri kendi kontrollerinde sınanır. | PASS |
| F-1 | `make verify-tickets` | Talep/durum akışı, sahiplik, tekrar anahtarı ve sayfalama doğru çalışır. | PASS |
| F-2 | `make verify-sla` | Hedef/takvim, ilk kabul, bekleme/devam ve çözüm akışı çalışır. | PASS |
| T-1 | `make verify-sla` ve `make prove-sla` | İş takvimi, tatil, %80/ihlal/eskalasyon, tekilleştirme ve takvim değişimi doğrulanır. | PASS |
| F-3 | `make verify-assignment` | Beceri/vardiya/kapasiteye uygun atama, kuyruk ve geçmiş çalışır. | PASS |
| T-2 | `make prove-assignment` | İki eşzamanlı HTTP isteği tek boş kapasiteyi aşamaz; izin/vardiya değişimi devre yol açar. | PASS |
| COMMENTS | `make verify-comments` | İç not çalışandan saklanır; tekrar ve kapalı kayıt kuralları korunur. | PASS |
| EMAIL | `make verify-email` | Yeni posta/yanıt, Message-ID, alıntı/imza ve ek yetkileri doğrulanır. | PASS |
| THREADS | `python3 scripts/verify.py --threads` | Birleştirme/bölme, mesaj konumu ve gizlilik korunur. | PASS |
| T-3 | `make verify-t3` | Canlı API akışları ve 21 gerçek PostgreSQL e-posta/birleştirme/bölme senaryosu geçer. | PASS |
| F-4 | `make verify-knowledge` | Taslak gizliliği, yayın, arama ve eski sürüm reddi çalışır. | PASS |
| F-5 | `make verify-assets` | Varlık/bağımlılık, yön, döngü, sürüm ve yetki kontrolleri çalışır. | PASS |
| F-6 | `make verify-changes` | Bağımsız CAB onayı, ortak etki/pencere çakışması ve yeniden onay çalışır. | PASS |
| T-4 | `make verify-t4` ve `make prove-t4` | Graf geçişi, eşzamanlı onay yarışı, eski graf reddi ve hata geri alma doğrulanır. | PASS |
| F-7 | `make verify-reports` | Yönetim yetkisi, açılış tarihine göre filtre, dağılımlar ve güncel iş yükü tutarlıdır. | PASS |
| SIMULATOR | `make verify-simulator` ve `make prove-simulator` | Küçük canlı çalışma; izole DB'de tam 800 talep, tekrar, devam etme ve profil koruma doğrulanır. | PASS |

## Ek komutlar ve kanıt kapsamı

- `make test`: Docker'da Go testleri ve race detector. DB bağlantı ayarı yoksa PostgreSQL grupları SKIP olur; bu komut tek başına entegrasyon kanıtı değildir.
- `make test-integration`: gerçek PostgreSQL üzerinde tüm Go testleri ve race detector. Her çağrı ayrı proje kullanır; ana demo DB'ye bağlanmaz.
- `make prove-sla`: mevcut hedef tüm entegrasyon paketlerini çalıştırır; yalnız SLA paketiyle sınırlı değildir.
- `make prove-knowledge`, `make prove-reports`, `make prove-assets`, `make prove-simulator`: ilgili izole PostgreSQL kanıtları.
- `make lint`: Go biçimi ve golangci-lint.
- `make verify-openapi`: OpenAPI 3.0.3 sözleşmesinin bağımsız doğrulaması. İlk çalışmada paket indirmek için internet gerekir.
- `make load`: SLA preview ucuna k6 yük testi. Kaydedilmiş 10 VU / 20 saniye sonucunda 3.930 hesaplama, sıfır hata, p95 0,62544 ms. Ölçüm kapsamı: [LOAD-RESULTS.md](docs/LOAD-RESULTS.md).
- `make measure-indexes`: ayrı sentetik DB üzerinde iki gerçek sorgunun indeksli/indekssiz planlarını üretir; ana DB değişmez. [INDEXES.md](docs/INDEXES.md).
- [requests.http](requests.http): OpenAPI'deki 51 işlemin her biri için elle istek örneği. Oturum çerezlerini saklayan REST Client gerekir; oluşturulan kayıt kimlikleri/sürümleri sonraki örneklere uyarlanır.

Kapsam ve ölçüm sınırları için [teslim doğrulaması](docs/DELIVERY-CHECKS.md), [CI](docs/CI.md), [API](docs/API.md) ve modül belgelerine bakın. Eski günlere ait süreler yeni ölçüm gibi yorumlanmamalıdır.

## Arayüzde elle deneme

1. Çalışanla giriş yap; yanlış parola hatasını, yenilemede oturumu ve çıkışı dene. Talep aç; detayda kategori, öncelik, atama ve SLA bilgilerini gör.
2. Uzmanla talebi işleme al, paylaşılan mesaj ve iç not ekle. Çalışana dön: yalnız paylaşılan mesaj görünmeli. Çalışan başka çalışanın talebini okuyamamalı; API kanıtı otomatik testtedir.
3. Uzmanla kullanıcı bekleniyor → işlemde → çözüldü → kapalı akışını dene. Beklemede çözüm sayacı durmalı, devamda bütçe korunmalı. Kapalı talepte yeni mesaj/durum değişimi olmamalı.
4. Liderle uzman panosunu aç; aktif yük/kapasite ve vardiyaları gör. Bir uzmanı izne al; işler devredilmeli veya kuyrukta kalmalı. Deneme sonunda izin ayarını geri al; devir geçmişi korunur.
5. Uzmanla makale taslağı aç; çalışan görmemeli. Liderle yayımla; çalışan arayıp okuyabilmeli. Tekrar taslağa alınınca çalışan yeniden yayımlanana kadar görmemeli.
6. Lider/admin e-posta simülatöründe dosyalı posta işle. Aynı Message-ID/içerik çift talep açmamalı. Yeni Message-ID ile Re: #numara yanıtı mevcut talebe mesaj olmalı. Birleştirme/bölmede iç not ve ek erişimi korunmalı.
7. Varlık yöneticisiyle varlık ve sağlayıcı → bağımlı bağlantısı ekle. Etki listesi aşağı bağımlıları göstermeli; döngüde hesap bitmeli.
8. Bakım taslağı açıp CAB'a gönder. Planı oluşturan kendi planını onaylayamamalı. Farklı lider/admin onay verebilmeli; ortak etki ve örtüşen pencere ikinci onayı engellemeli. Graf değişince yeniden inceleme/onay istenmeli.
9. Liderle rapor tarih/kategori filtrelerini ve boş sonucu dene. Uzman iş yükünün tarih filtresinden bağımsız güncel olduğunu kontrol et. Çalışan/uzman rapora erişememeli.
10. Üretici çalışırken lider/admin Simülasyon ekranını aç; ilerleme beş saniyede bir yenilenmeli. Yeni veri üretmek için `make simulate` kullanılır; aynı tamamlanmış plan çoğalmaz. Bu komut gerçek demo verisi ve vardiya değişimi üretir.
11. Tema menüsünde Sistem/Açık/Koyu seçeneklerini dene. Elle seçim yenilemede korunmalı; mobil genişlikte ekran kullanılabilir olmalı. Yükleniyor, boş ve hata durumlarını da kontrol et.

Kaydedilmiş elle kabul kontrolü temel ekran, rol ve iç not gizliliği akışlarını kapsar. CAB'ın bütün karar dalları elle yürütülmüş sayılmaz; ayrıntılı kapsam [teslim doğrulamasında](docs/DELIVERY-CHECKS.md) belirtilir.

## İsteğe bağlı otomatik tarayıcı kontrolleri

Playwright 1.50.1, Chromium ve Node gerekir. Bunlar uygulamanın üretim bağımlılığı değildir. Araçları ayrı bir dizine kurarak:

```bash
npm install --prefix /tmp/destekmasa-browser playwright@1.50.1
/tmp/destekmasa-browser/node_modules/.bin/playwright install chromium
NODE_PATH=/tmp/destekmasa-browser/node_modules COLOR_SCHEME=dark node scripts/browser-smoke.cjs
```

Aynı NODE_PATH ayarıyla HTTP IP erişimi için `node scripts/browser-http-login.cjs` güvenli bağlam olmayan adreste açık/koyu 390px ve altı rol giriş/çıkışını kontrol eder. Gerçek adresle denemek için BASE_URL ayarlanabilir. Talep oluşturmaz; oturum ve istemci UUID üretimini sınar.

Diğer akışlar: browser-assets.cjs, browser-changes.cjs, browser-knowledge.cjs, browser-reports.cjs, browser-simulator.cjs. browser-theme.cjs sistem tercihi, elle seçim, kalıcılık, sekmeler arası eşitleme, depolama engeli ve mobil görünümü sınar. İsteğe bağlı AXE_PATH ile kontrast taraması yapılır. BASE_URL, DEMO_PASSWORD ve BROWSER_ARTIFACT_DIR ayarlanabilir. Betikler veri oluşturur; ayrı demo ortamı kullanın. Ekran görüntüleri varsayılan /tmp/destekmasa-browser-artifacts altındadır.

## Sıfırlama ve durdurma

```bash
# Verileri koruyarak durdur.
docker compose down

# YALNIZ demo verilerini silmek istiyorsanız:
make reset-demo CONFIRM=yes
```

Onaysız `make reset-demo` veri silmeden başarısız olur. Onaylı komut projeye ait volume'ü siler, yeniden başlatıp seed durumunu kurar; oturumlar ve oluşturulmuş kayıtlar kaybolur. Normal güncellemede reset gerekmez. Bu davranış ayrı ortamda test edildi; ana demo sıfırlanmadı.

## Bonus durumu

- Uzman yük/kapasite panosu ve raporda güncel uzman iş yükü: mevcut. Çoklu takım ayrımı yoktur.
- Benzer taleplere göre otomatik bilgi bankası önerisi: yok. Normal makale araması vardır.
- SLA ihlal tahmini: yok. Mevcut %80 uyarısı belirli süre eşiğine dayanır; öngörü modeli değildir.
