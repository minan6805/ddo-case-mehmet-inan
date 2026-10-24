# DDO Projesi — Mehmet İnan

DDO IT yardım masası projesi. **Geliştirme aşamasında: servis bağlantısı, giriş/çıkış, rol kontrolü, temel talep akışı, iş takvimli SLA, otomatik atama motoru ve görünürlük kontrollü talep mesajları ve T-3 e-posta/birleştirme/bölme akışı.**

## Kurulum

Gerekenler: Docker ve Docker Compose. Yerel Go/Node kurulumu gerekmez. İlk derlemede internet gerekir.

```bash
cp .env.example .env
docker compose up --build
```

Mevcut `.env` dosyan varsa kopyalama adımını atla; aşağıdaki demo/oturum ayarları tanımlı değilse Compose yerel varsayılanları kullanır.

- Arayüz: http://localhost:3001
- Backend sağlık ucu: http://localhost:8080/health
- PostgreSQL host'a açılmaz; Compose ağından erişilir.
- Başlatma: PostgreSQL hazır → API migrasyon/demo seed → arayüz.
- Durdurma: `docker compose down`. Veriler Docker volume içinde kalır.

**3001 geçicidir:** yerelde 3000 dolu. Teslimden önce şartnamedeki 3000'e dönülecek.

`.env` Git'e eklenmez. `DEMO_SEED=true`, eksik demo kullanıcıları otomatik oluşturur; mevcut parolaları değiştirmez. `DEMO_PASSWORD` varsayılanı `DemoDestek2026!`; yerel demo içindir. `SESSION_TTL=8h` oturum süresini belirler. `COOKIE_SECURE=false` yerel HTTP içindir; HTTPS ortamında true olmalı. Demo seed kapatılabilir (`DEMO_SEED=false`). Bu ayarlar internet ortamına üretim kurulumu yapıldığı anlamına gelmez.

## Demo hesapları

Aşağıdaki hesapların ilk oluşturulurken kullanılan ortak yerel parolası: **`DemoDestek2026!`**. `DEMO_PASSWORD` değiştirilmişse ilk seed sırasında seçilen parola geçerlidir.

| Rol | E-posta |
|---|---|
| Çalışan | calisan@destekmasa.local |
| Destek uzmanı | uzman@destekmasa.local |
| Destek uzmanı 2 | uzman2@destekmasa.local |
| Destek uzmanı 3 | uzman3@destekmasa.local |
| Takım lideri | lider@destekmasa.local |
| Varlık yöneticisi | varlik@destekmasa.local |
| Yönetici | admin@destekmasa.local |

## Mimari ve gerekçeler

```text
Tarayıcı → React / Nginx → Go HTTP API → PostgreSQL 16
              /api/... → /...       → kullanıcılar, oturumlar, denetim
```

Go standart HTTP kütüphanesiyle başlıyoruz. pgx, SQL ve transaction'ları açıkça yönetmemizi sağlıyor. React + TypeScript ekran durumlarını ve veri tiplerini ifade ediyor; Vite derliyor. Nginx derlenmiş dosyaları sunuyor ve API'ye yönlendiriyor; tarayıcı aynı adrese istek yapıyor.

Girişte bcrypt parola özeti doğrulanır. Kriptografik olarak rastgele 32 baytlık oturum anahtarı HttpOnly/SameSite=Strict çereze yazılır; veritabanında yalnızca SHA-256 özeti tutulur. Her korumalı istekte geçerlilik ve güncel kullanıcı rolü veritabanından okunur. Tekrar giriş eski tarayıcı oturumunu döndürür; çıkış oturumu siler. Başarılı giriş/çıkış ve denetim kaydı aynı transaction içinde tamamlanır. JSON giriş zorunluluğu, Origin kontrolü ve SameSite çerez politikası yabancı sitelerden yazma isteklerini sınırlar.

Kararlar: [DECISIONS.md](DECISIONS.md).

## SLA motoru

Talep detayında ilk yanıt ve çözüm sayaçları bulunur. İş takvimi hafta içi 08.30–18.00, Europe/Istanbul; hafta sonu ve tanımlı tam/yarım tatiller sayılmaz. İlk işleme alma kabul yanıtını tamamlar; kullanıcı yanıtı beklenirken çözüm sayacı durur. %80 uyarısı, ihlal ve takım liderine uygulama içi eskalasyon kayıtları kalıcı ve tekilleştirilmiştir. Yönetici takvim ve kategori/öncelik hedeflerini arayüzden düzenler.

Takvim düzenlemesi tüm durum geçmişini aynı transaction'da yeniden hesaplar; hedef değişikliği yeni taleplere uygulanır. Detaylı kurallar, erişim, kanıt ve sınırlar: [docs/SLA.md](docs/SLA.md). 2026 tatilleri [Borsa İstanbul kaynağından](https://www.borsaistanbul.com/resmi-tatil-gunleri) doğrulandı; sonraki yıllar yönetici tarafından eklenmeli.

## Otomatik atama

Kategori becerilerinin tamamını karşılayan, vardiyası açık, izinli olmayan ve boş kapasitesi bulunan uzmanlar değerlendirilir. Puan %50 beceri/%30 boş kapasite/%20 round-robin sırasıdır. Aktif yük ve atama aynı kilitli transaction içinde korunur. Uygun uzman yoksa talep kuyrukta kalır. İzin, vardiya, beceri veya kapasite değişiminde işler devredilir; karar ve eski/yeni uzman geçmişte saklanır.

Talep detayında atanan uzman; destek ekranında kişisel/atanmamış kuyruk filtresi ve atama geçmişi; lider/admin için kapasite/beceri/vardiya/izin panosu var. Demo uzman vardiyaları hafta sonu da açık; SLA bundan bağımsızdır. Kurallar, yarış kanıtı ve sınırlar: [docs/ASSIGNMENT.md](docs/ASSIGNMENT.md).

## Mevcut durum ve sınırlar

Hazırlananlar: Compose/healthcheck, migrasyonlar, yedi demo hesap, bcrypt/DB oturumları, giriş/çıkış ve rol kontrolü. Talep açma, sahipliğe göre liste/detay, kategori/öncelik, durum filtresi ve destek ekibi için durum geçişleri API ve arayüzde çalışıyor. Oluşturma Idempotency-Key ile korunuyor; kritik değişiklikler audit kaydıyla aynı transaction'da yazılıyor. Uçlar `requests.http` içinde.

Talep erişimi: her rol kendi adına talep açabilir. Çalışan ve varlık yöneticisi yalnızca kendi taleplerini okur. Destek uzmanı, takım lideri ve yönetici ortak destek kuyruğunu görür ve durum değiştirir. Henüz takım/vardiya bazlı kuyruk ayrımı yok.

Durum akışı: açık → işlemde → yanıt bekleniyor → işlemde → çözüldü → kapalı. Çözülen talep işlemde durumuna dönebilir; kapalı talep tekrar açılmaz. Paylaşılan mesajlar ve destek iç notları hazır. Talep silme ve açıklama düzenleme henüz yok.

**Henüz yok:** varlık grafı, değişiklik akışı, bilgi bankası, raporlar, günlük 800 taleplik genel simülatör, CI/lint ve OpenAPI. Sınırlı e-posta simülatörü hazır. Rol kontrolünün altyapısı ve örnek uç hazır; gelecekteki her işlev kendi erişim kurallarını uygulamalı.

Oturumlar sabit sürelidir; otomatik yenileme ve periyodik süresi dolmuş oturum temizliği henüz yok. Giriş hızı sınırlaması, parola değiştirme ve hesap yönetimi henüz yok. Audit trigger'ları normal UPDATE/DELETE/TRUNCATE işlemlerini reddeder; veritabanı yöneticisinin trigger'ları devre dışı bırakmasına karşı koruma iddiası yok.

24 Ekim 2026 doğrulaması: Docker imajları derlendi, üç ana servis healthy. Go birim testleri `-race`, `go vet` ve TypeScript/Vite derlemesi başarılı. Gerçek PostgreSQL'de 11 kimlik + 24 talep/yorum + 11 SLA + 16 atama + 11 e-posta + 10 birleştirme/bölme = **83 entegrasyon senaryosu PASS**. Canlı API'de HEALTH/WEB/AUTH/RBAC/F-1/F-2/F-3/T-1/T-2/T-3/COMMENTS/EMAIL/THREADS kontrolleri başarılı. Chromium ile talep ve SLA kabul/bekleme/devam akışı, yönetici takvim/hedef kaydı, 390px mobil düzen, iç notların çalışandan gizlenmesi, paylaşılan yanıtlar, izinle otomatik devir, atama geçmişi ve çıkış doğrulandı. Tam proje doğrulaması kalan F/T görevleri nedeniyle henüz geçmez.

Kısa k6 ölçümü: 10 VU, 20 saniye; SLA preview ucunda 3.930 hesaplama isteği, HTTP hata %0, p95 0,62544 ms. Bu küçük yerel hesaplama senaryosudur; tüm uygulama veya takvim yeniden hesaplama kapasitesini göstermez. Kapsam ve sonuç: [docs/LOAD-RESULTS.md](docs/LOAD-RESULTS.md).

`go.sum` ve `package-lock.json` oluşturuldu. Docker derlemesi kilit dosyalarını kullanır (`go mod download`, `npm ci`). Yerel doğrulama araçları sistem kurulumunu değiştirmeden `/tmp/destekmasa-tools` altında çalıştırıldı.

## Doğrulama

`make smoke` temel bağlantı, `make verify-auth` canlı giriş/yetki, `make verify-tickets` temel talep akışı, `make verify-sla` SLA API, `make prove-sla` kalıcı SLA kanıtı, `make load` kısa k6 ölçümü, `make verify-assignment` canlı atama API ve T-2 PostgreSQL kanıtı, `make prove-assignment` yalnız atama PostgreSQL grubu, `make test-integration` ayrı PostgreSQL ile testlerdir. `make verify` tamamlanmamış zorunlu özellikleri FAIL gösterir; bu aşamada teslim doğrulaması başarılı değildir.

## Talep mesajları ve iç notlar

Talep detayında “Mesajlar ve notlar” alanı var. Çalışan kendi talebine paylaşılan mesaj ekler; uzman/lider/admin ayrıca “İç not” seçebilir. İç notlar çalışanın API yanıtına dahi girmez. Metinler HTML olarak çalıştırılmaz; kapalı talebe yeni mesaj yazılmaz. Aynı gönderim anahtarıyla yeniden denemek çift kayıt oluşturmaz.

`make verify-comments` çalışan sistemde görünürlük, sahiplik, tekrar/çakışma ve kapanış kurallarını doğrular. Mesajlar uygulamada kaydedilir; dışarıya e-posta gönderimi yoktur. Mesaj eklemek durumu veya SLA sayacını değiştirmez; ilk yanıt hâlâ açık “İşlemde” kabulüyle kaydedilir. Ayrıntılar: [docs/COMMENTS.md](docs/COMMENTS.md).

## E-posta, birleştirme ve bölme (T-3)

Lider/admin ekranındaki “E-posta simülatörü” kayıtlı kullanıcı adına yeni talep veya `#talepNo` konulu yanıt üretir; dosya ekleyebilirsin. Aynı Message-ID tekrar işlendiğinde çift kayıt açılmaz. `make simulate-email` aynı akışı ayrı Docker servisinden, aynı tohumla tekrar üretir. Gerçek SMTP/IMAP bağlantısı yoktur.

Talep detayında lider/admin “Talebi birleştir veya böl” alanından aynı kişiye ait kayıtları birleştirir veya seçilen mesajları yeni talebe ayırır. İç notlar gizli kalır; ekler korunur. Bölmede yeni SLA başlar; birleştirmede hedefin süresi korunur. İşlem geçmişi detayda görünür.

`make verify-t3` API ve gerçek PostgreSQL kanıtını çalıştırır. Teknik kurallar, sınırlar ve simülatör: [docs/EMAIL.md](docs/EMAIL.md). Genel doğrulamada artık T-3 PASS; kalan F-4…F-7 ve T-4 nedeniyle tüm proje henüz hazır değildir.
