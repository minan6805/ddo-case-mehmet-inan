# SLA motoru — kurallar, kanıt ve sınırlar

## Kullanıcı açısından

SLA, ilk yanıt ve çözüm için verilen iş süresi hedefidir. Talep detayında iki ayrı sayaç bulunur. Varsayılan takvim İstanbul saatine göre hafta içi 08.30–18.00'dır; öğle arası kesintisi yoktur. Hafta sonu ve tanımlı tatiller süreye eklenmez.

Cuma 17.30'da başlayan 60 iş dakikalık hedefin sonu pazartesi 09.00'dır: cuma 30 dakika, pazartesi 08.30'dan itibaren 30 dakika.

Bu sürümde uzman/lider/admin tarafından talebin **ilk işleme alınması**, kullanıcıya verilen kabul yanıtıdır. Talep detayında bu kabulün tarihi gösterilir. Bu bir e-posta veya serbest metin yorumunun gönderildiği anlamına gelmez; gelen e-posta ve yorum akışı T-3 kapsamında mevcuttur. Dışarıya e-posta teslimi uygulanmaz.

Çözüm sayacı:

- `open` ve `in_progress`: iş saatleri içinde sayar.
- `waiting_customer`: durur; bekleme boyunca yeni kesin son tarih gösterilmez.
- `resolved` ve `closed`: durur ve tamamlanma zamanı gösterilir.
- Çözülen talep yeniden işleme alınırsa önceki tüketim korunarak aynı hedeften devam edilir; çözülmüş kaldığı süre sayılmaz.
- İlk kabul yanıtı bir kere tamamlanır; yeniden açılmada bu sayaç başlamaz.

## Takvim ve hedef düzenleme

Yönetici, destek listesinin altındaki “SLA takvimi ve hedefleri” alanından saatleri, tam gün/13.00 sonrası tatilleri ve kategori/öncelik hedeflerini yönetir. Uzman ve lider ayarları okuyabilir; değiştiremez. Çalışan ve varlık yöneticisi yalnızca kendi taleplerinin SLA'sını okuyabilir.

2026 takvimi [Borsa İstanbul'un resmî tatil takviminden](https://www.borsaistanbul.com/resmi-tatil-gunleri) doğrulandı. Arifeler: 19 Mart, 26 Mayıs ve 28 Ekim saat 13.00 sonrası; bunları tam gün tatil saymak yanlış olur. **Sonraki yılların tatilleri otomatik indirilmez; yönetici tarafından eklenmeli.**

Takvim değişikliği geçmiş durum aralıklarını yeni takvimle yeniden değerlendirir; tüketim ve hedef zamanları aynı transaction içinde tüm talepler için yeniden hesaplanır. Sürüm kontrolü eski ekranın yeni ayarı ezmesini engeller (409). Hedef/politika değişikliği ise yalnızca yeni talepleri etkiler: mevcut talebin açılış hedefi korunur.

## Teknik çalışma

`internal/sla/calendar.go` veritabanına ihtiyaç duymayan hesap modülüdür. Yerel günlerin çalışma pencereleri ile zaman aralıklarının kesişimini hesaplar. Dakika dakika dolaşmaz; gün gün ilerler. Saat dilimi için IANA verisi kullanılır; takvim gününde ilerlemek için sabit 24 saat yerine `AddDate` kullanılır. Böylece yaz/kış saati geçişleri de hesaba katılır. API zamanları UTC, SLA ekranı İstanbul saatidir.

`ticket_status_history` başlangıç ve durum değişikliklerini tutar. Durumlar yeniden oynatılarak yanıt ve çözüm tüketimi bulunur. Saniye altı hassasiyet hesap içinde korunur; ekrandaki toplamlar saniye/dakikaya indirgenir. İhlalden sonra son tarih her kontrol anına kaydırılmaz; özgün hedef anı geçmiş etkin aralıklardan bulunur.

`ticket_sla` hedefin açılış anındaki kopyasını ve son projeksiyonu tutar. Talep oluşturma/durum değişimiyle SLA kaydı aynı transaction'dadır. Okuma ucu yalnızca hesaplar; yan etkisi yoktur. Durum değişimi, takvim düzenlemesi ve arka plan kontrolü projeksiyonu/uyarıyı kalıcı yazar.

Takvim/politika yazıcıları global advisory lock'ı özel, talep işlemleri ve kontrol çalışanı paylaşımlı alır. Kilit sırası daima takvim → talep satırıdır. Kontrol çalışanı `FOR UPDATE SKIP LOCKED` kullanır; meşgul satırı beklemek yerine sonraki kayda geçer.

## Uyarı ve eskalasyon

- Hedefin %80'i tüketilince `warning`.
- Hedefin tamamı tüketilince `breach` ve hemen `escalation`.
- Eskalasyon alıcısı takım lideridir; talebin `escalation_level` değeri en az 1 olur ve audit kaydı yazılır.
- `(ticket_id,timer,stage)` benzersizdir: aynı eşik iki çalışan veya tekrar kontrol tarafından çoğaltılmaz.
- Uyarılar destek ekibinin “SLA uyarıları” listesine düşer. SLA eskalasyonu harici e-posta/SMS göndermez ve uzmanı değiştirmez; izin/vardiya devri ayrı atama motorunun işidir.
- Geçmiş uyarılar takvim değişince silinmez. Yeni hesap artık ihlal göstermese bile geçmiş eskalasyon kaydı kalır; olayın takvim sürümü saklanır.

Kontrol sıklığı `SLA_SWEEP_INTERVAL=10s`, grup büyüklüğü `SLA_SWEEP_BATCH=200`. Çok sayıda açık talepte tüm kuyruğun taranması birden fazla tur sürebilir. Detay ekranı 15 saniyede bir güncel hesabı ister; uyarı listesi “Yenile” ile güncellenir.

## Kanıt komutları

```bash
make verify-sla          # F-2/T-1 API hesap ve yaşam döngüsü
make prove-sla           # ayrı PostgreSQL ile gerçek kayıt/uyarı/eskalasyon kanıtı
make load                # k6, salt okunur SLA hesaplama ucuna yük
```

`make verify-sla` kontrollü zamanları `/sla/preview` hesaplama ucuna gönderir. Bu uç üretim saatini veya talepleri değiştirmez. Ayrıca gerçek talep açma, kabul yanıtı, bekleme, devam ve bitiş akışını API üzerinden dener. Takvim/hedef değerlerini değiştirmeden yeniden kaydeder: sürüm ve audit artar, eski sürümle kaydetme 409 olmalıdır. Demo DB'de bir `[SLA doğrulama]` talebi bırakır. Bu API kanıtı tek başına arka plan uyarılarının kalıcı kaydının kanıtı değildir; bunun için `make prove-sla` gerekir.

PostgreSQL SLA grubunda 11 senaryo: seed, hedef kaydı, sahiplik, %80/ihlal/eskalasyon tekilleştirme, ilk yanıt, bekleme/dönüş, çözüm/yeniden açma, politika kopyası, eşzamanlı kontrol çalışanları, takvim değişimi/tüm projeksiyonlar/audit ve uyarı sayfalama.

Bağımsız dakika sayan referansla 140 tarih/bütçe örneği karşılaştırılır. Cuma/hafta sonu, tam/yarım tatil, kapanış sınırı, UTC, Berlin saat değişimi, saniye altı sınır, bekleme, uyarı ve özgün ihlal hedefi ayrı testlerdir. Test saati yalnızca testte bağımlılık olarak verilir; üretim saatini değiştiren bir API yoktur.

24 Ekim 2026: 11 SLA PostgreSQL senaryosu PASS (0,61 s grup süresi), önceki kimlik/talep testleri de PASS; tümü yarış algılayıcısıyla. Canlı API F-2 ve T-1 PASS. Chromium'da SLA göstergeleri, kabul/bekleme/devam, yönetici takvim/hedef kaydı ve mobil ayarlar PASS. [Yük ölçümü](LOAD-RESULTS.md).

## Bilinen sınırlar

- Tek takvim ve pazartesi–cuma çalışma düzeni; vardiya bazlı farklı SLA takvimi yok.
- Tarih hesabı 10 yıllık aralıkla, hedefler 1–43.200 iş dakikasıyla sınırlı.
- Takvim düzenlemesi tüm geçmiş aralıkları mevcut tanıma göre yeniden hesaplar. Çok büyük veri hacminde tek transaction ve API zaman aşımı darboğaz olabilir; hata olursa takvim ve yeniden hesaplama birlikte geri alınır. Büyük veri için sürüm tabanlı asenkron yeniden hesaplama sonraki geliştirmedir.
- Seed tatilleri 2026 ile sınırlı. Sonraki yıllar ayrıca eklenmeli.
- İlk yanıtın tanımı kabul/işleme almadır. Serbest metin yanıt veya e-posta teslimini henüz ölçmez.
- Tek seviye eskalasyon ve uygulama içi uyarı; harici teslim garantisi yok.
