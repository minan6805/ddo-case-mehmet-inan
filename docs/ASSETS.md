# Varlık envanteri ve bağımlılık etkisi (F-5)

Varlık yöneticisi/admin varlık oluşturur, sürüm kontrollü düzenler ve bağımlılık ekler/kaldırır. Uzman/lider envanteri ve etkiyi okur. Çalışan bu modüle erişemez; rol kontrolü hem API hem servis katmanındadır. Varlıkta tekil kod, ad, tür, durum ve isteğe bağlı sorumlu kullanıcı bulunur. Fiziksel silme yerine kullanım dışına alma vardır; durum değişikliği bağlantıları otomatik silmez.

Bağlantı **sağlayıcı → bağımlı** yönündedir: sunucu → servis → uygulama. Etki hesabı seçilen kökü ve aşağı yönde ulaşılabilen bütün varlıkları içerir. Döngüler desteklenir; ziyaret edilmiş kümesi sonsuz dolaşmayı ve elmas biçimli grafikte tekrarları engeller. Birden çok kökten breadth-first traversal en kısa bağlantı mesafesini verir. ID sırası ile kararlı bir örnek yol seçilir. Yukarı yöndeki sağlayıcılar arızadan etkilenmiş sayılmaz. Etki listesi olası yayılımdır; kesin kesinti garantisi değildir.

Graf okuması repeatable-read transaction içinde varlık, bağlantı ve graf sürümünü aynı anlık görüntüden okur. Her gerçek varlık/graf değişikliği global graf sürümünü artırır. Yazıcılar 7420184 advisory kilidinde sıraya girer; bu kilit değişiklik/CAB kararlarıyla da paylaşılır. Sürüm kontrolü kayıp düzenlemeyi önler. Aynı bağımlılık PUT/DELETE tekrarı audit veya sürümü gereksiz artırmaz. Oluşturma kullanıcı kapsamlı Idempotency-Key ile korunur. Varlık/graf değişikliği, tekrar yanıtı ve audit tek transaction'dadır.

Başlangıç demoları ağ → sunucu → kimlik servisi → portal zinciridir. Seed yalnız ilk başlangıçta bu zinciri ekler; sonraki başlangıçlar düzenlenmiş/silinmiş bağlantıları geri getirmez. Var olan kullanıcı şifreleri de korunur.

Sınırlar: tam etki hesabı en fazla 5000 düğüm/20000 bağlantı; detayda her yönde en fazla 1000 doğrudan komşu. Aşılırsa eksik sonucu doğruymuş gibi döndürmek yerine hata verilir. Graf görseli ilk 50 varlığı çizer, etki listesi tüm hesaplanan varlıkları gösterir. Büyük listelerde ID azalan keyset sayfalama vardır. Aktif/bakımda/kullanımdan kaldırılmış bütün varlıklar açıkça kaldırılana kadar topolojide kalır; bu, eksik güncellenmiş envanterde yayılımı gizlemeyen muhafazakâr seçimdir.

## Uçlar ve kanıt

- `GET/POST /assets`, `GET/PUT /assets/{id}`; POST Idempotency-Key, PUT güncel version gerektirir.
- `GET /assets/owners`: yalnız varlık yöneticisi/admin, sorumlu seçimi için sayfalı kullanıcı listesi.
- `PUT/DELETE /assets/{id}/dependencies/{dependent}`: kaynak sağlayıcıdır.
- `GET /assets/{id}/impact`: gerçek graf üzerinden kök dahil etki.
- `make verify-assets`: çalışan API'de roller, CRUD, tekrar, eski sürüm, döngü ve yön kanıtı. Doğrulama varlıkları bağlantısız ve kullanımdan kaldırılmış bırakılır.
- `make prove-assets`: geçici ayrı PostgreSQL'de 10 senaryo. Seed koruma, sürüm yarışı, sekiz eşzamanlı tekrar, döngü/elmas, kenar tekrarı ve audit hatasında geri alma dahil.
- `internal/assets` saf hesap testi, 100 farklı sekiz düğümlü grafikte bütün kökleri bağımsız Floyd–Warshall referansıyla karşılaştırır.
- `scripts/browser-assets.cjs`: Playwright ile varlık/bağımlılık oluşturma, döngüden etki, 390px mobil görüntü ve temizlik kanıtı; Node ve Playwright gerekir.
