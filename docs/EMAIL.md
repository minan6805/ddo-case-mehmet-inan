# T-3 — E-postadan talep, yanıt, ek ve talep düzenleme

## Gelen e-posta

Lider/admin, arayüzde “E-posta simülatörü”nü açar. Gönderen kayıtlı bir kullanıcıdır. Konuda talep numarası yoksa genel kategori/normal öncelikle yeni talep açılır; aynı web akışındaki atama ve SLA motorları çalışır. Konuda `#123` varsa mevcut talebe paylaşılan mesaj eklenir. Başka çalışanın numarasını yazmak erişim sağlamaz: gönderen talep sahibi veya destek rolünde olmalı. Bilinmeyen gönderen reddedilir. Kapalı talebe yeni yanıt eklenmez.

`POST /email/ingest`, oturum ve lider/admin rolü gerektirir; gövde `message/rfc822` olur. Bu güvenilir demo girişinde lider, From alanıyla kullanıcı adına veri üretir. Gerçek SMTP/IMAP bağlantısı, SPF/DKIM doğrulaması, dışarıdan kabul edilen kimlik doğrulanmamış posta ve dışarıya posta gönderimi yoktur. Bu uç normal çalışanlara açık değildir.

Message-ID zorunlu ve tekildir. Aynı kimlik + aynı ham baytlar tekrar geldiğinde 200 ve `duplicate:true`; ilk kayıtta 201 döner. Aynı kimlikle farklı baytlar 409 verir. Kimlik birleştirme/bölmeden sonra da korunur; tekrar yanıtı ilgili mesajın şu an bulunduğu talebi gösterir. `created_ticket`, ilk işlemin yeni talep açıp açmadığını anlatır.

İşlem tek transaction'dır: gönderici kontrolü, talep/yorum, atama, SLA, ekler, Message-ID makbuzu ve audit birlikte kaydedilir. Kilit sırası takvim → atama → talep satırıdır. Sekiz eşzamanlı aynı e-posta bir talep, bir yorum ve bir ithalat audit kaydı üretir. Ekin kaydı hata verirse kısmi talep dahi bırakılmaz.

## Ayrıştırma ve ek sınırları

Go `net/mail`, `mime`, `mime/multipart`, base64 ve quoted-printable ayrıştırıcıları kullanılır. UTF-8 ve US-ASCII düz metin desteklenir. Multipart içindeki HTML görünüm atlanır; HTML-only posta ve desteklenmeyen karakter setleri reddedilir. İç içe MIME derinliği 8, toplam parça 40 ile sınırlıdır. Ham posta en fazla 4 MiB; her ek 1 MiB, toplam ek 2 MiB, en fazla 10 dosyadır. Temizlenen metin 1–10000, konu 1–200 Unicode karakterdir. Yalnız ek varsa açıklama için sabit bir metin kullanılır.

Alıntı satırları (`>`), bilinen “On … wrote:”/“… yazdı:” ve özgün ileti ayraçları temizlenir. `-- `, “Saygılarımla,” gibi bilinen imza ayraçlarından sonrası alınmaz. Bu sınırlı kurallar her posta istemcisinin bütün imzalarını tanımaz; keyfi kelimeler imza sayılmaz.

Ekler PostgreSQL bytea olarak saklanır; boyut ve SHA-256 kaydedilir. Dosya adı yol parçalarından/kontrol karakterlerinden temizlenir; dosya sistemine yazılmaz. `GET /attachments/{id}` oturumla erişilir ve mevcut talep + yorum görünürlüğünü kontrol eder. Yanıt zorunlu `attachment`/`application/octet-stream`, `nosniff`, `no-store` ve sandbox başlıkları taşır. HTML dosyası uygulama içinde çalıştırılmaz. Virüs tarama servisi henüz yoktur.

## Birleştirme

`POST /tickets/{id}/merge`, JSON `target_id`, zorunlu Idempotency-Key; yalnız lider/admin. Kaynak ve hedef aynı talep edenin kayıtları olmalı, kapalı/birleştirilmiş olmamalı ve birbirinden farklı olmalıdır. Kaynak kapanır, yorumlar hedefte görünür, ekler yorumla birlikte erişilir. Yorumun yazar/metin/görünürlük/özgün talep alanları değişmez. Özgün açıklama kapalı kaynakta korunur; işlem geçmişi kaynağa bağlantı verir.

Hedefin SLA hedefleri, geçen süresi ve durumu değişmez; kaynak sayacı kapanır, aktif kapasite serbest kalır. Kaynağa daha önce birleştirilmiş numaralar doğrudan yeni hedefe bağlanır; zincir/döngü oluşmaz. Eski `#numara` ile gelen e-posta yanıtı hedefe yönlenir ve gönderen kontrolü yeniden uygulanır. Kaynağa web üzerinden yeni mesaj yazmak kapanış nedeniyle reddedilir.

## Bölme

`POST /tickets/{id}/split`, JSON `title`, `comment_ids`, zorunlu Idempotency-Key; yalnız lider/admin. Açık/işlemde/bekleyen/çözülmüş kayıttan bu talebe ait 1–100 farklı mesaj seçilir. Aynı talep sahibi, kategori ve öncelikle yeni açık talep oluşur; yeni SLA ve bağımsız atama başlar. Kapasite doluysa yeni iş kuyrukta kalır. Kaynağın durumu ve SLA bütçesi korunur.

Yeni açıklama sabit ve genel bir metindir: seçilmiş iç notun gövdesi açık açıklamaya kopyalanmaz. Başlığı yönetici belirler ve bu başlık talep edene görünür. Paylaşılan yorumlar paylaşılan, iç notlar gizli kalır; ek erişimi de aynı kurala bağlıdır. Yorumların asıl satırları değişmez; `comment_locations` yalnızca şu an hangi talepte olduklarını tutar. Yanlış talebe ait/seçilmeyen kimlik işlem yapamaz. Eşzamanlı iki farklı bölme aynı mesajı iki kez taşıyamaz; aynı anahtarla tekrar aynı sonucu verir.

Birleştirme/bölme geri alma API'si yoktur. `ticket_operations` geçmişi ve audit birlikte kaydedilir; geçmiş normal UPDATE/DELETE/TRUNCATE ile değişmez. `GET /tickets/{id}/operations` sahiplik kontrollü keyset sayfalama sunar; iç not gövdelerini/kimlik listesini içermez.

## Simülatör ve kanıt

`make simulate-email`, ayrı Python Docker servisini bir kez çalıştırır. Varsayılan tohum 42, çalışma adı demo-v1, talep sayısı 3'tür. Her konu için ilk posta + aynı postanın tekrarı + konu numaralı yanıt gönderilir; ilk postada ek bulunur. Aynı tohum/çalışma adı/aynı simülatör sürümü aynı MIME baytlarını üretir. Yeniden çalıştırınca yeni kayıt açılmaz. Bağımsız bir seri için SIMULATOR_RUN değiştirilebilir; sayım 1–50 ile sınırlıdır.

24 Ekim 2026 iki çalıştırma: ilki 3 talep + 3 yanıt; ikincisi 0 yeni talep, bütün iletiler tekrar olarak işlendi. Makbuzlar gerçek DB kimliklerini taşır; farklı boş veritabanlarının sayısal kimliklerinin eşit olması beklenmez. Bu sınırlı e-posta vericisidir; proje bölüm 8'deki günlük 800 talep, vardiya ve varlık verisi üreten genel simülatör henüz tamamlanmadı.

`make verify-t3`: çalışan API'de e-posta, yorum görünürlüğü, birleştirme/bölme ve ayrı PostgreSQL'de 21 T-3 entegrasyon senaryosu. `make prove-t3` yalnızca bu PostgreSQL gruplarını çalıştırır. `make verify` bütün entegrasyon gruplarını bir kez çalıştırıp T-2 ve T-3 kanıtını aynı çıktıdan doğrular. Toplam 83 PostgreSQL senaryosu PASS. Ayrıştırıcı için ayrı birim testleri de bulunur. Chromium kanıtında dosyalı e-posta, Message-ID tekrarı, indirme, bölme/birleştirme ve çalışan hesabında iç not gizliliği doğrulandı.

Mesaj eklemek otomatik durum değişikliği yapmaz; bekleyen/çözülmüş talep otomatik yeniden açılmaz. SLA ilk yanıtı mevcut “İşlemde” kabulüdür. Gelen e-posta kaydı dışarıya yanıt teslimi veya okundu bilgisi anlamına gelmez.
