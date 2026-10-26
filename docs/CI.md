# Otomatik kontroller

[CI workflow](../.github/workflows/ci.yml), `main` gönderimlerinde, pull request'lerde ve GitHub Actions ekranından elle başlatıldığında çalışır. Üç iş paralel yürür:

| İş | Kontroller |
|---|---|
| Go | Biçim, bağımlılık doğrulama, API/simülatör derlemesi, golangci-lint ve gerçek PostgreSQL üzerinde race testleri |
| Arayüz | Kilit dosyasıyla `npm ci`, TypeScript kontrolü ve Vite derlemesi |
| Compose | Örnek ayarlarla servislerin başlatılması ve F/T kimlikleri ile simülatörü içeren `make verify` |

Her iş kendi GitHub runner ortamını kullanır. Test veritabanları ve demo servisleri bu ortamlarda oluşturulur; Compose işi çıkışta kendi volume'lerini temizler. Örnek ayarlar `.env.example` üzerinden alınır. Workflow depo içeriğini okuma izniyle çalışır; kullanılan action'lar commit SHA'sına sabitlenmiştir. Action çalışma ortamı Node.js 24'tür; arayüzün derleme ortamı Node.js 22 olarak tanımlıdır.

Go işinde setup-go önbelleği kapalıdır; runner üzerinde hazır bulunan salt okunur Go modülleriyle önbellek arşivinin çakışması önlenir. Derleme, lint ve bütün testler her çalıştırmada yürütülür.

## Lint

```bash
make lint
```

Yerelde Docker yeterlidir. Komut Go biçimini ve golangci-lint v2.1.6'yı kontrol eder. GitHub'daki lint adımı aynı sürümü ve [backend/.golangci.yml](../backend/.golangci.yml) ayarlarını kullanır. Sürüm değiştirilecekse Makefile ve workflow birlikte güncellenmelidir.

Standart denetleyiciler: `errcheck`, `govet`, `ineffassign`, `staticcheck` ve `unused`. Hata listesi sayı sınırıyla kesilmez. Ayrıntılar: [golangci-lint belgeleri](https://golangci-lint.run/docs/welcome/quick-start/).

Başarılı commit'ten sonra yapılan en iyi çaba rollback temizliğinin dönüş değeri kodda açıkça göz ardı edilir; commit'in kendi hatası normal şekilde ele alınır. Testlerin trigger/kilit temizliği hataları ise testi başarısız yapar.

## Sonuçları okuma

GitHub'da bir commit'in yanındaki kontrol durumundan veya deponun Actions sekmesinden üç işin sonucuna ulaşılır. Başarısız adım, düzeltilecek komutu ve test çıktısını gösterir. Docker işinde başarısızlık olursa API ve arayüzün son logları da yazılır.

Otomatik kontrollerin geçmesi işlevsel davranış ve derleme için kanıttır. Kullanıcı kabul testleri, temiz kurulum süresi, OpenAPI ve indeks performans ölçümleri ayrı teslim adımlarıdır.

Entegrasyon betiği her çağrıda ayrı bir `destekmasa-tests-<zaman>-<pid>` Compose projesi oluşturur. Uygulamanın `COMPOSE_PROJECT_NAME` değişkeni bu adı değiştiremez. Böylece iki kanıt komutu aynı anda çalışırken birbirlerinin PostgreSQL konteynerlerini durdurmaz. Çıkışta ve INT/TERM kesintilerinde betik kendi projesini temizler; test derleme imajı önbellek için ortak kalır.
