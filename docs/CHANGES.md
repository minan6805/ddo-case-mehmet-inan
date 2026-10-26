# Değişiklik yönetimi ve graf etkisi (F-6 / T-4)

## Plan ve yetki

Uzman, lider, varlık yöneticisi ve admin planları okuyabilir ve kendi adına taslak açabilir. Çalışan bu modüle erişemez. Plan başlığı, uygulama açıklaması, geri dönüş planı, risk, başlangıç/bitiş ve 1–20 kök varlık gerektirir. Kökler mevcut ve kullanım dışına alınmamış olmalıdır. Bir pencere en fazla 7 gündür; taslak oluşturma/güncellemede bitiş gelecekte, başlangıç en fazla 366 gün ileride olmalıdır. Saatler API'de RFC3339, DB'de timestamptz/UTC, ekranda kullanıcının yerel saatidir.

Taslağı yalnız plan sahibi veya admin düzenler. Gönderilmiş/onaylanmış plan doğrudan düzenlenmez; önce taslağa alınır ve eski onay geçersiz olur. CAB kararı plan sahibinden farklı bir lider/admin tarafından verilir. Admin de kendi planını onaylayamaz. Bu sürüm tek bağımsız karar kullanır; çok üyeli oy çoğunluğu veya otomatik risk bazlı kurul seçimi yoktur.

Durumlar:

- Taslak → CAB'a gönder → Onay bekliyor.
- Bağımsız CAB: onayla, düzeltme iste (taslağa döner) veya reddet.
- Onaylandı → yalnız pencere içindeyken uygulamayı başlat → uygulanıyor → tamamlandı/başarısız.
- Plan sahibi/admin gönderilmiş veya onaylanmış planı taslağa alabilir; taslak/gönderilmiş/onaylanmış planı iptal edebilir.
- Uygulanmaya başlamış iş taslağa dönmez veya iptal edilmez; sonuç notuyla tamamlanır/başarısız kapatılır. Sonuç daha geç girilebilir.

Her karar en az 3, en fazla 2000 karakter açıklama ve güncel kayıt sürümü ister. Uygulama/geri dönüş komutları uzak sunucuda otomatik çalıştırılmaz; ekip bu işleri gerçekleştirip sonucu kaydeder.

## Etki, çakışma ve eşzamanlılık

CAB'a gönderilirken köklerden aşağı yönde ulaşılabilen tüm varlıklar hesaplanır; kökler, döngüler ve ortak bağımlılar dahil tekrarsız bir anlık etki kaydı saklanır. Ayrı kökleri olan iki plan aynı uygulamaya ulaşıyorsa ortak etki alanları vardır. Yalnız doğrudan köklerin eşitliğine bakılmaz.

Zaman pencereleri `[başlangıç,bitiş)` olarak değerlendirilir; birinin bittiği anda başlayan iş çakışmaz. Onaylı ve uygulanıyor durumundaki işler rezervasyon sayılır. Ortak etki varlığı ve örtüşen pencere varsa ikinci onay reddedilir. Tamamlanmış/başarısız/reddedilmiş/iptal edilmiş işler yer tutmaz. Gönderilmiş taslaklar rezervasyon değildir; onay sırasında tekrar kontrol zorunludur.

Graf düzenleme, gönderim, CAB kararı ve uygulama başlangıcı aynı PostgreSQL advisory kilidini (7420184) kullanır. Önizleme repeatable-read, yazılar kilit altında transaction'dır. Bu yüzden iki eşzamanlı HTTP onay isteği aynı boş aralığı kapamaz. Sürüm kontrolü aynı plana gelen çakışan düzenlemeleri de reddeder. Kontrol, bu protokolü kullanan uygulama yazıcıları için geçerlidir; doğrudan yetkili SQL değişikliklerini bir exclusion constraint ile bütünüyle engelleme iddiası yoktur.

Graf sürümü CAB'a sunulan sürümden farklıysa onay/başlatma reddedilir. Plan sahibi taslağa alıp yeniden gönderir ve yeni bağımsız onay ister. Eski grafla onaylı bir işin rezervasyonu otomatik silinmez: aynı zaman penceresindeki yeni onaylar, kökler farklı görünse bile muhafazakâr biçimde engellenir. Önce eski plan yeniden incelenmeli veya iptal edilmelidir. Varlığın yalnız adı/sorumlusu veya aynı alanlarla yeniden kaydedilmesi de graf sürümünü değiştirdiğinden yeniden inceleme isteyebilir. Bu küçük sistemde güvenilir ama geniş kapsamlı bir geçersizleştirme tercihidir.

`change_impacts` ortak varlık kontrolü için, GiST tstzrange indeksi aktif pencere araması için kullanılır. Etki hesabı [varlık grafının](ASSETS.md) boyut sınırlarına tabidir. Çakışma yanıtı ilk 101 kaydı gösterir; herhangi bir çakışma onayı engeller. Önizleme bir rezervasyon veya sonradan başarı garantisi değildir.

## Kayıt bütünlüğü

Oluşturma ve karar POST'ları kullanıcı/işlem kapsamlı Idempotency-Key ile tekilleştirilir. Aynı anahtar/gövde ilk yanıtı döndürür; farklı içerik 409 olur. Başarılı oluşturma tekrarının pencere geçtikten sonra da ilk makbuzu alması desteklenir. İzinler tekrar yanıtından önce kontrol edilir.

Plan, kökler, etki kaydı, onay, tekrar yanıtı, işlem geçmişi ve audit aynı transaction'a aittir. Audit hatası onayı ve rezervasyonu da geri alır. Karar geçmişi UPDATE/DELETE/TRUNCATE ile değiştirilemez. Audit önceki/yeni durumları ve plan alanlarını tutar; büyük etki gövdesini tekrar kopyalamaz. Tam etki kaydı ilgili plan üzerinde korunur. Uygulama üzerinden terminal plan veya geçmiş düzenlenemez; DB yöneticisinin korumaları kaldırmasına karşı kriptografik koruma yoktur.

## API ve kanıt

- `GET/POST /changes`: durum filtresi ve azalan ID keyset listesi; POST Idempotency-Key.
- `GET/PUT /changes/{id}`: detay/CAB'a sunulan etki ve taslak düzenleme; PUT version.
- `GET /changes/{id}/impact`: güncel etki, grafın eskimiş olması ve mevcut rezervasyon çakışmaları; salt okunur.
- `POST /changes/{id}/actions`: `action`, `note`, `version`, Idempotency-Key.
- `GET /changes/{id}/history`: erişim kontrollü, sayfalı karar geçmişi.
- `make verify-changes`: canlı API'de bağımsız onay, ortak bağımlı çakışması, erken başlatma reddi, graf değişimi, yeniden onay ve geçmiş.
- `make verify-t4`: F-5/F-6 API kontrolleri + izole PostgreSQL ve saf graf kanıtı.
- `make prove-t4`: ayrı PostgreSQL'de 10 varlık ve 15 değişiklik senaryosu, ayrıca saf graf testleri. Demo DB silinmez.
- `scripts/browser-changes.cjs`: iki gerçek tarayıcı hesabıyla taslak, CAB, çakışma, pencere içinde uygulama/tamamlama, eski graf reddi ve 390px mobil kontrolü.

24 Ekim 2026: iki eşzamanlı HTTP CAB onayından biri 200, diğeri 409; DB'de tek onaylı rezervasyon. Ayrıca farklı köklerdeki ortak aşağı varlık, bitişe bitişik pencereler, ayrık etki alanları, eski grafla onay/başlatma reddi, taslağa dönüş, süresi geçmiş makbuz tekrarı ve audit hatasında tam geri alma PostgreSQL ile doğrulandı. Bu aşamada toplam 108 PostgreSQL senaryosu vardı. Sonraki bilgi bankası, rapor ve simülatör eklemeleriyle güncel toplam 141 oldu; 26 Ekim genel doğrulamasında 20 kimliğin tamamı PASS verdi.
