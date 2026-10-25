# Bilgi bankası

Bilgi bankası, destek ekibinin hazırladığı çözüm adımlarını çalışanların arayıp okuyabileceği makaleler halinde sunar. Yeni kurulumda üç yayımlanmış örnek makale oluşturulur. Yeniden başlatma, makalelerdeki değişiklikleri geri almaz.

## Erişim ve yayın

| Rol | Okuma | Yazma |
|---|---|---|
| Çalışan, varlık yöneticisi | Yalnız yayımlanmış makaleler | Yok |
| Destek uzmanı | Tüm durumlar | Kendi taslağını oluşturma/düzenleme |
| Takım lideri, yönetici | Tüm durumlar | Tüm makaleleri düzenleme, yayımlama ve arşivleme |

Oturum zorunludur. Taslak ve arşivlenmiş makaleler yetkisiz okuyucuya liste/arama yanıtında girmez; doğrudan numarayla erişim 404 döner. Yazar kimliği oturumdan alınır.

Her yeni makale taslaktır. Yayımlanmış içerik doğrudan değiştirilemez: lider/yönetici önce içeriği değiştirmeden taslağa alır; düzenleme ve yeniden yayın bundan sonra yapılır. Arşivden yayına dönüş de taslak üzerinden olur. Lider/yönetici kendi makalesini yayımlayabilir; bu modül CAB'deki bağımsız onay kuralını kullanmaz. Silme yoktur; arşivlenen makale ve audit geçmişi korunur.

Başlık 1–200, içerik 1–10.000 karakterdir. Kategori mevcut talep kategorilerinden seçilir. Metin düz yazı olarak gösterilir; HTML çalıştırılmaz. Başlık ve içerik araması PostgreSQL `lower` ile büyük/küçük harf dönüşümü ve gerçek alt metin eşleşmesi kullanır; `%` ve `_` joker değildir. Türkçe dil kökü, aksan eşleme ve tam metin sıralaması uygulanmaz; arama performansı büyük koleksiyonlar için ayrıca ölçülmelidir.

## API

- `GET /knowledge?q=VPN&category=network&status=published&limit=20&before=123`: son kimlikten geriye doğru sayfalama. Yanıt `{items, next_cursor}`. Sınır 1–100; arama en çok 200 karakter.
- `GET /knowledge/{id}`: `{article}`.
- `POST /knowledge`: `Idempotency-Key` zorunludur; aynı kullanıcı/anahtar/içerikle tekrar aynı oluşturma yanıtını döndürür. Başka içerik 409. Oluşturma yanıtı ilk taslak sürümüdür; güncel kayıt GET ile alınır.
- `PUT /knowledge/{id}`: başlık, içerik, kategori, durum ve mevcut `version`. Eşzamanlı düzenlemelerden yalnız biri kazanır; eski sürüm 409. Otomatik yeniden uygulama yapılmaz.

Yazma gövdesi:

```json
{"title":"VPN bağlantısı","body":"Bağlantınızı kontrol edin.","category":"network","status":"draft","version":0}
```

Yeni kayıtta sürüm 0, düzenlemede GET yanıtındaki sürüm kullanılır. Makale ve audit aynı transaction içindedir; audit yazılamazsa makale değişikliği ve oluşturma anahtarı da geri alınır. Audit, eski ve yeni içerikleri saklar; herkese açık audit okuma ucu yoktur.

## Doğrulama

- `make verify-knowledge`: çalışan uygulamada taslak gizliliği, rol kontrolü, yayın, arama, eski sürüm ve yeniden taslak akışı; oluşturulan makale sonunda arşivlenir.
- `make prove-knowledge`: ayrı PostgreSQL şemasında seed koruması, yaşam döngüsü, yetkiler, arama/sayfalama, eşzamanlı oluşturma/düzenleme, HTTP kontrolleri ve audit hatasında geri alma.
- `scripts/browser-knowledge.cjs`: Playwright ile uzman taslağı, çalışan gizliliği, lider yayını, arama, düz metin ve mobil görünüm.
