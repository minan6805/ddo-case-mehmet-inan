# Mimari kararlar

## ADR-001 — Tek backend uygulaması

**Bağlam:** Küçük ekip, kısa teslim süresi ve ortak PostgreSQL işlemleri.

**Seçenekler:** Mikroservisler veya modüllere ayrılmış tek uygulama.

**Karar:** Tek Go uygulaması; HTTP ve veritabanı kodu ayrı paketlerde. İş kuralları geliştikçe kendi paketlerine ayrılacak.

**Sonuç:** Tek derleme ve kolay işlem yönetimi; paket sınırlarını korumak gerekiyor.

## ADR-002 — SQL ve pgx

**Bağlam:** Kapasiteyi eşzamanlı koruma ve sorgu performansını kanıtlama gerekiyor.

**Seçenekler:** ORM, SQL üreten araçlar veya pgx ile doğrudan SQL.

**Karar:** pgx ve açık SQL; numaralı migrasyonlar API başlatılırken transaction ve PostgreSQL advisory lock altında uygulanır. Uygulanmış dosyanın checksum'ı değişirse başlangıç durur.

**Sonuç:** Kilitler ve sorgular görünür olur. Başlangıç migrasyonları küçük tutulmalı. İlk aşamada geri alma komutu yok; yeni düzeltme migrasyonu eklenir. Şema ve roller aynı başlangıç transaction'ında oluşturulur.

## ADR-003 — Aynı adres üzerinden arayüz ve API

**Bağlam:** Arayüzün gerçek API verisini Docker ortamında kullanması gerekiyor.

**Seçenekler:** Ayrı origin ve CORS veya Nginx üzerinden aynı origin.

**Karar:** React + TypeScript + Vite; Nginx `/api/` önekini kaldırıp Go API'ye yönlendirir.

**Sonuç:** Tarayıcı Docker servis adlarını bilmez. Geliştirme için önerilen yol Compose'dur; yerel Vite dev sunucusu için proxy henüz yapılandırılmadı.

## ADR-004 — Sunucuda saklanan oturum

**Bağlam:** Kimlik doğrulama, anında çıkış/iptal ve güncel role göre erişim kontrolü gerekiyor. Mevcut uygulama aynı origin üzerinden çalışıyor.

**Seçenekler:** Access/refresh JWT veya sunucuda saklanan rastgele oturum kimliği.

**Karar:** PostgreSQL'de oturum; tarayıcıda HttpOnly/SameSite=Strict çerez. Parolalar bcrypt; rastgele oturum kimliğinin yalnızca SHA-256 özeti veritabanında. Yerel HTTP için Secure false, HTTPS için yapılandırmayla true. Her istekte kullanıcı rolü DB'den okunur; başarılı girişte önceki tarayıcı oturumu silinir.

**Sonuç:** Çıkış ve rol değişikliği hemen geçerli olur; access/refresh yenileme akışı gerekmez. Her korumalı istekte DB sorgusu gerekir. Sabit süre sonunda tekrar giriş gerekir. Çerez kullandığımız için yazma isteklerinde Origin kontrolü ve SameSite politikası uygulanır. Giriş yalnızca JSON kabul eder.

## ADR-005 — İzole entegrasyon veritabanı

**Bağlam:** Gerçek PostgreSQL testi gerekir; kullanıcının demo verisi testlerden etkilenmemeli.

**Seçenekler:** Mevcut demo veritabanı veya ayrı Compose projesi ve geçici DB.

**Karar:** `compose.test.yaml` ayrı proje ve tmpfs PostgreSQL başlatır. Test kendi rastgele şemasını oluşturup kaldırır; `INTEGRATION_DATABASE_URL` verilmezse PostgreSQL testi açıkça SKIP olur.

**Sonuç:** Yerel demo korunur; entegrasyon testi için Docker gerekir. Test komutu PostgreSQL hatalarını başarısız çıkış koduyla iletir. `make test-integration` Go yarış algılayıcısını da açar; bu, T-2 kapasite yarışının henüz kanıtlandığı anlamına gelmez.

## ADR-006 — Talep oluştururken tekrar isteği koruma

**Bağlam:** Ağ zaman aşımı veya çift tıklama aynı POST isteğini yeniden gönderebilir. Eşzamanlı iki istek de aynı kaydı oluşturmamalı.

**Seçenekler:** Sadece önceden var mı sorgusu; istemcide düğmeyi devre dışı bırakma; veritabanında benzersiz işlem anahtarı ve transaction.

**Karar:** `(user_id, operation, key)` birincil anahtarı; normalleştirilmiş istek gövdesinin SHA-256 özeti ve yanıtın tamamı saklanır. INSERT ON CONFLICT ve satır kilidi aynı anahtarlı işlemleri sıraya koyar. Talep, audit kaydı ve yanıt birlikte commit edilir. Aynı içerik eski yanıtı, farklı içerik 409 döndürür. Geçersiz işlemde tüm transaction geri alınır. PATCH durum isteği mevcut durumu tekrar isterse değişiklik/audit tekrarlanmaz.

**Sonuç:** Aynı anahtarlı sekiz eşzamanlı istek PostgreSQL testinde tek talep ve tek audit üretir. Anahtar farklı kullanıcılarda bağımsızdır. Arayüz aynı içerikle yeniden denemede anahtarı korur. Kayıt temizliği henüz yok. Eski POST yanıtı oluşturma anının görüntüsüdür; son durum GET ile okunur.

## ADR-007 — Talep erişimi, durum geçişi ve sayfalama

**Bağlam:** Taleplerin sahipliği korunmalı, destek ekibi işleyebilmeli ve büyük listelerde OFFSET kullanılmamalı.

**Karar:** Çalışan/varlık yöneticisi kendi taleplerini; uzman/lider/admin ortak kuyruğu görür. Talep sahibi istemci girdisinden alınmaz, doğrulanmış oturumdan gelir. Durum geçişleri sunucudaki açık bir tabloda doğrulanır; satır kilidi altında güncellenir ve audit yazılır. Liste `id DESC`, sonraki sayfa `id < before`; bir fazla kayıtla sonraki sayfanın varlığı belirlenir.

**Sonuç:** Doğrudan API isteğiyle sahiplik/rol aşılamaz. Liste için `(requester_id,id DESC)` ve `(status,id DESC)` indeksleri var; performans ölçümleri henüz yok. İş kuyruğu henüz takım/atanan uzmanla sınırlandırılmıyor; bu T-2 ile genişletilecek. Kapalı talep yeniden açılmaz; çözülen talep işleme dönebilir. SLA takibi bu akışa sonraki aşamada bağlanacak.

## ADR-008 — Durum geçmişinden iş süresi hesaplama

**Bağlam:** Bekleme süreleri dışlanmalı; takvim değişince tüketim ve hedef yeniden hesaplanabilmeli. Sadece bir son tarih alanı bunu açıklamakta yetersiz.

**Seçenekler:** Duvar saatiyle son tarih; her saniye azaltılan sayaç; durum geçmişini iş takviminde yeniden oynatma.

**Karar:** Değişikliklerin zaman çizelgesi ve başlangıç hedef kopyası saklanır. Saf hesap modülü yerel gün pencerelerinde ilerler; hafta sonu/tam/yarım tatili dışlar. İlk işleme alma kabul yanıtını tamamlar. Bekleme ve çözülmüş kalma aralıkları çözüm sayacına eklenmez. Çözülmüşten işleme dönünce aynı bütçe sürer.

**Sonuç:** Hesap tek bir saate bağımlı değildir; testte kontrollü zaman ve bağımsız referansla kanıtlanır. SLA tarihleri GET sırasında güncel hesaplanır; projeksiyon ve olay kaydı ayrı olarak transaction'da tutulur. İlk yanıt tanımı işleme alma/kabuldür; gerçek metin/e-posta teslimi T-3'e kalır.

## ADR-009 — Takvim yeniden hesaplama ve politika kopyası

**Bağlam:** Takvim düzeltmesi mevcut talepleri etkilerken, yeni süre hedefi mevcut verilmiş taahhütleri sessizce değiştirmemeli.

**Karar:** Takvim düzenlemesi tüm tarihçeyi yeni sürümle aynı transaction'da yeniden hesaplar. Politikaların bütçesi açılışta talebe kopyalanır; değişiklik yalnızca yeni taleplere uygulanır. Sürüm kontrolü kayıp güncellemeyi reddeder. Global takvim kilidi daima talep satırından önce alınır; normal işlemler paylaşımlı, ayar yazıcıları özel kilit alır.

**Sonuç:** Takvim ve projeksiyon atomik değişir, kilit sırası tutarlıdır. Büyük veri hacminde tek transaction'ın süresi bir sınırdır. Eski uyarılar olay geçmişi olarak korunur; otomatik silinmez veya eskalasyon geri alınmaz. 2026 tatilleri seed edilir, gelecekteki yıllar yönetici tarafından eklenir.

## ADR-010 — Kalıcı ve tekilleştirilmiş SLA olayları

**Bağlam:** Aynı eşiği tekrar kontrol etmek veya iki API örneği çalıştırmak çift uyarı üretmemeli.

**Karar:** Periyodik kontrol, SKIP LOCKED ile sınırlı grup seçer. Her timer/eşik için benzersiz olay yazılır. %80 uyarısı, %100 ihlali ve takım liderine birinci seviye eskalasyon aynı işlemde kaydedilir. Durum değişimi de eşik kontrolü yapar; kontrol turları arasındaki geçiş kaçırılmaz.

**Sonuç:** Uygulama içi uyarı kalıcıdır; dış e-posta teslimi yoktur. İlk grup/kontrol varsayılanı 200/10 saniyedir; tüm kuyruğun gecikmesi açık kayıt sayısına bağlıdır. Tekrar ve çoklu çalışan PostgreSQL testleri çift olayı engellediğimizi kanıtlar.

## ADR-011 — Kilitli yük gözlemi ve açıklanabilir atama puanı

**Bağlam:** İki talep aynı son kapasite yerini kullanmamalı; beceri, vardiya, yük ve round-robin birlikte değerlendirilmelidir.

**Seçenekler:** Kilitsiz COUNT; profil başına kilit + yeniden deneme; bütün kararları sıraya alan transaction kilidi.

**Karar:** Proje ölçeğinde tek PostgreSQL atama kilidi, aday profil satır kilitleri ve yazımdan önce son kapasite kontrolü. Yük aktif taleplerden sayılır; ayrı sayaç tutularak drift yaratılmaz. Puan %50 beceri/%30 boş kapasite/%20 sıra, tam sayıyla hesaplanır; uygunluk filtreleri puandan önce uygulanır. Sıra global artan atama numarasıyla izlenir.

**Sonuç:** Kapasite yarışı iki gerçek eşzamanlı HTTP isteği ve 24 eşzamanlı oluşturmayla kanıtlandı. Oluşturma/durum/profil/worker aynı kilit sırasını kullanır. Global karar kilidi ölçek sınırıdır; doğrudan SQL ile protokolü atlayan yazıcı desteklenmez. Skor bileşenleri geçmişte saklanır. Geçerli atama sırf yeni skor farklı diye taşınmaz.

## ADR-012 — Kuyruk ve devir, durum/SLA'dan bağımsızdır

**Bağlam:** Uygun uzman olmayabilir; izin ve vardiya değişiminde iş kaybolmamalı. Atama, gerçek kabul yanıtının yerine geçmemeli.

**Karar:** Atanamayan aktif talep ayrı kuyrukta tutulur; otomatik atama talep durumunu değiştirmez. İzin/beceri/vardiya/kapasite değişiminde mevcut işler yeniden değerlendirilir. Bekleyen kullanıcı yanıtı yük tüketir; çözüm/kapalı yük tüketmez. Yeniden açılma kapasiteyi tekrar sınar. Eski/yeni uzman, neden ve puan append-only geçmiş ve audit'te kalır.

**Sonuç:** Kapasite açılınca kuyruk doldurulur; uygun yedek yoksa görev atamasız kalır ve SLA devam eder. Üç demo uzmanı her gün açık vardiyayla seed edilir; böylece hafta sonu atama denenir, SLA iş takvimi değişmez. Tek günlük vardiya penceresi ve anlık izin var; planlı izin ve çoklu takım yok. Destek ekibinin ortak kuyruğu işleme yetkisi sürer; “bana atananlar” güvenlik sınırı değildir.

## ADR-013 — Yorum görünürlüğünü sunucuda uygula, gönderimi tekilleştir

Talep edenin yanıtına iç notları hiç dahil etmiyoruz; görünürlük filtresi sayfalama sınırından önce SQL'de uygulanır. Yazar oturumdan alınır. Yorum yazma talep satırını kilitler; kapanışla sıralanır. Kullanıcı/talep kapsamlı Idempotency-Key, yorum ve audit aynı transaction'dadır. Böylece bağlantı sonrası tekrar ve eşzamanlı gönderim tek kayıt verir. Kayıtlar append-only; audit metni kopyalamaz. Mesajlar durum/SLA değiştirmez; gerçek e-posta teslimi henüz uygulanmadı. Bu yorum temelinin ardından ADR-014/015 ile T-3 e-posta ve düzenleme akışı tamamlandı.

## ADR-014 — Güvenilir e-posta simülatörü, atomik Message-ID ve sınırlı MIME

Gerçek posta altyapısı yerine lider/admin oturumu gerektiren message/rfc822 ithalatı ve ayrı, deterministik Docker e-posta vericisi seçildi. Normal çalışan From alanıyla kullanıcı taklidi yapamaz. Gönderen DB'de bulunur, yanıtın talep erişimi kontrol edilir. Message-ID + ham SHA-256, tek transaction'da talep/yorum/ek/atama/SLA/audit ile kaydedilir. Aynı kimlik/ham baytlar tekrar, farklı baytlar 409'dur. UTF-8 düz metin ve yaygın MIME ekleri desteklenir; HTML-only/diğer karakter setleri reddedilir. Ekler küçük boyut sınırlarıyla DB'de tutulur, görünürlük kontrollü zorunlu indirme olarak sunulur. Dışarıya e-posta teslimi ve genel günlük 800 olay simülatörü kapsamı henüz tamamlanmadı.

## ADR-015 — Değişmez yorum ve ayrı güncel konumla birleştirme/bölme

Yorum satırını taşımak immutable geçmişi bozar; kopyalamak çift kimlik ve ek erişimi sorunları üretir. Özgün yorum değişmez, comment_locations güncel talebi tutar. Okuma ve ek yetkisi bu konuma göre değerlendirilir. Birleştirme yalnız aynı talep sahibinde yapılır; kaynak kapanır, eski alias'lar düzleştirilir, hedef SLA korunur ve kapasite boşalır. Bölme yeni SLA/atamalı iş açar; açıklama iç nottan üretilmez. Tüm işlem mevcut takvim → atama → talep kilit sırasıyla, Idempotency-Key ve append-only işlem geçmişi/audit ile transaction içindedir. Geri alma API'si yoktur. Birleştirme/bölmeden sonra e-posta makbuzunun tekrar yanıtındaki bağlantı mesajın güncel konumuna gider.
