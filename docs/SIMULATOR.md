# Günlük iş yükü simülatörü

Simülatör, aynı uygulama kurallarını kullanan ayrı bir Go/Compose servisidir. API'nin kullandığı talep, SLA, atama, e-posta ve varlık servislerini çağırır; sahte tablo satırlarıyla bu kuralları atlamaz. PostgreSQL bağlantısı yalnız Compose ağı içindedir. Çalışması için demo seed açık ve demo admin/çalışan/ilk iki uzman hesabı mevcut olmalıdır.

```bash
make simulate
```

Varsayılan çalışma 800 talebi bir günlük akış olarak iki dakikaya yayar. Derleme ve veritabanı işlem süresi buna eklenebilir. Docker servisinin bitmesi beklenirken lider/admin ile **Simülasyon** sekmesinden ilerleme beş saniyede bir izlenebilir. Talepler, raporlar ve varlıklar gerçek uygulama verisini gösterir.

## Üretilen akış

Varsayılan 800 talep için:

- 640 doğrudan talep ve 160 e-postadan yeni talep.
- 160 e-posta yanıt zinciri ve 160 küçük örnek ek dosya.
- Yeni e-postaların ve yanıtların birer tekrarı: 320 tekrar tesliminde yeni kayıt açılmadığı kontrol edilir.
- 40 destek iç notu; farklı kategori ve öncelikler; açık/işlemde/bekleyen/çözüldü/kapalı durumları.
- İki demo uzmanda sabah/öğleden sonra vardiyası ve geri dönüşü içeren dört vardiya adımı. Atama motoru uygun olmayan uzmanların işlerini devreder veya kuyruğa alır. Kapasite sınırı kaldırılmaz.
- Çalışmaya özel kodlarla dört varlık, sağlayıcıdan bağımlıya zincir ve sonlu etki hesabını göstermek için bir döngü. Mevcut envanter kayıtları değiştirilmez.

Normal bitişte uzmanların önceki profil ayarları geri alınır. Talepler ve işlem geçmişleri demo verisi olarak kalır. Vardiya değişimi ortak destek kuyruğunu etkileyebilir; bu nedenle kullanıcı kabul testleriyle aynı anda çalıştırılmamalıdır.

## Ayarlar ve zaman

| Değişken | Varsayılan | Anlam |
|---|---|---|
| `SIMULATOR_RUN` | `demo-v1` | 1–32 ASCII harf/rakam/tire içeren çalışma adı |
| `SIMULATOR_DAY` | `2026-10-26` | Planın mantıksal günü; `YYYY-MM-DD` |
| `SIMULATOR_SEED` | `42` | 0–2147483647 arası tohum |
| `SIMULATOR_COUNT` | `800` | Bir günün talep sayısı; kısa deneme için 4–800 |
| `SIMULATOR_DAY_SECONDS` | `120` | Akışın yayılacağı gerçek saniye; 0 beklemesiz, en çok 86400 |

Ayarlar `.env` üzerinden veya servis ortam değişkenleriyle verilir. Örneğin küçük ve yeni bir çalışma:

```bash
docker compose --profile tools run --rm \
  -e SIMULATOR_RUN=kisa-demo \
  -e SIMULATOR_COUNT=8 \
  -e SIMULATOR_DAY_SECONDS=0 simulator
```

Her gün yeni veri üretmek için farklı `SIMULATOR_DAY` seçilir; servis kendiliğinden günlük zamanlama yapmaz. 86400 saniye, 800 talebi 24 saate yayar. Mantıksal gün, kayıtların `created_at` veya sistem saatini değiştirmez. Talep/SLA işlemleri gerçek saate göre çalışır; hafta sonu çalıştırmak iş günü süresi tüketmiş gibi davranmaz.

## Tekrarlanabilirlik ve devam

Ad + gün + tohum çalışma kimliğidir. Tüm ayarlarla oluşturulan işlem planı SHA-256 özetiyle saklanır. Aynı ayarlar aynı başlık, açıklama, kategori/öncelik seçimleri, e-posta içeriği, işlem sırası ve göreli zamanlamayı üretir. Gerçek oluşturulma saatleri, artan veritabanı numaraları ve mevcut yüke bağlı uzman seçimleri çalışma ortamına bağlıdır.

- Tamamlanmış çalışma aynı ayarlarla yeniden çalıştırıldığında veri eklenmez ve geçmiş yeniden oynatılmaz.
- Aynı kimlik farklı sayı/süreyle kullanılırsa reddedilir; yeni bir çalışma adı seçilmelidir.
- Her işlemden sonra checkpoint saklanır. İşlem tamamlanıp checkpoint yazılamazsa tekrar; oluşturma anahtarı, Message-ID veya mevcut hedef durum üzerinden güvenle denenir.
- Checkpoint yazılmadan gerçekleşen bir işlem, ilerleme sayacında henüz görünmeyebilir. Başarılı devam sonunda toplam hedefle eşleşir.
- Veritabanı oturum kilidi aynı anda yalnız bir üreticiye izin verir. İkinci süreç veri yazmadan hata verir. Bu kilit talep/atama transaction kilitlerinin yerine geçmez.
- SIGTERM veya işlem hatasında bilinen simülatör profil değişiklikleri geri alınır. Beklenmeyen dış profil değişikliği ezilmez; `PROFILE_CHANGED` gösterilir. Kullanıcı profil farkını incelemeli, uygun ayarı kendisi belirlemeli ve aynı komutla devam etmelidir.
- Zorla öldürme veya veritabanı bağlantısının kaybında anında temizlik garanti edilemez. Aynı çalışma devam ettirilince checkpoint'teki profil senaryosu sürdürülür ve sonunda ilk ayarlar geri alınır. Uzun süredir heartbeat gelmiyorsa ekran “Durum güncellenmiyor” gösterir.

Simülatörün kullandığı talepleri çalışma sırasında kullanıcı eliyle kapatmak/birleştirmek veya profil ayarlarını değiştirmek planla çakışabilir. Bu durumda işlem başarısız görünür; sessizce farklı veri üretmez. Geri alınan vardiyalar önceki atama kararlarını geriye sarmaz; bu kararların geçmişi korunur.

## API ve doğrulama

Durum uçları salt okunur, lider/admin erişimine açıktır:

- `GET /simulations?limit=10&before=123`
- `GET /simulations/{id}`

Checkpoint ve özgün profil kopyaları API yanıtına girmez. Çalıştırma, yapılandırma ve durdurma Docker servisi üzerinden yapılır.

- `make verify-simulator`: ayrı Compose üreticisini 8 talepte çalıştırır, durum API'sini/yetkileri kontrol eder ve PostgreSQL kanıtını çalıştırır. Sabit doğrulama adı tekrar çalıştırmada yeni kayıt oluşturmaz.
- `make prove-simulator`: ayrı test veritabanında tam 800 talep; e-posta/ek/durum sayıları; tamamlanmış tekrarın veri değiştirmemesi; checkpoint öncesi kesintiden devam; vardiya geri alma; dış profil değişikliğini koruma; dışarıdan çözülmüş talebi yeniden açmama; eşzamanlı üreticiyi reddetme.
- `make verify`: işlevsel kontrollerle birlikte simülatör kanıtını da içerir.
- `scripts/browser-simulator.cjs`: rol kontrolü, sayaçlar, otomatik yenileme, mobil görünüm, boş/eski/hatalı veri durumları. Canlı ilerlemeyi zorunlu sınamak için üretici çalışırken `EXPECT_RUNNING=1` verilir.

Küçük, yalnız e-posta örneklerini üreten `make simulate-email` komutu da ayrı olarak kullanılabilir.
