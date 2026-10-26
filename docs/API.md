# API sözleşmesi

Sözleşmenin tek kaynağı [openapi.json](../backend/internal/httpapi/openapi.json) dosyasıdır. OpenAPI 3.0.3 biçimindedir; JSON, bu standardın desteklediği belge biçimlerinden biridir. 42 yol üzerindeki 51 işlemi, istek/yanıt şemalarını, durum kodlarını, sayfalamayı ve rol koşullarını içerir.

API dosyayı derleme sırasında gömer; çalışma anında dosya sistemi, harici bir belge sunucusu veya internet bağlantısı gerekmez:

```bash
curl http://localhost:8080/openapi.json
curl http://localhost:3000/api/openapi.json
```

Dosya OpenAPI destekleyen bir Swagger görüntüleyicisine yüklenebilir. Bu proje harici CDN kullanan bir dokümantasyon ekranı içermez. Çalışan istek örnekleri [requests.http](../requests.http) içindedir.

## Oturum ve yetki

`POST /auth/login`, `destekmasa_session` adlı HttpOnly/SameSite=Strict çerezini verir. Sonraki istekler bu çerezi taşır; Bearer token kullanılmaz. CLI örneği:

```bash
curl -c /tmp/destekmasa-cookies -H 'Content-Type: application/json' \
  -d '{"email":"calisan@destekmasa.local","password":"DemoDestek2026!"}' \
  http://localhost:8080/auth/login
curl -b /tmp/destekmasa-cookies http://localhost:8080/tickets
curl -b /tmp/destekmasa-cookies -X POST http://localhost:8080/auth/logout
rm /tmp/destekmasa-cookies
```

`x-roles`, işlem düzeyindeki rolleri belirtir; sahiplik, makalenin yayın durumu ve bağımsız CAB kararı gibi ek koşullar işlem açıklamasındadır. Rol listesi tek başına tüm kaynaklara erişim vermez. Oturum yoksa 401, rol/sahiplik uygun değilse 403 döner. Bilgi bankasında gizli makale 404 ile saklanır. Sağlık kontrolü, giriş/çıkış ve sözleşme indirme oturum gerektirmez.

Yazma isteklerinde Origin varsa aynı host olmalıdır; cross-site istekler reddedilir. Origin göndermeyen CLI istemcileri desteklenir. JSON istekleri `application/json`; e-posta içe aktarma `message/rfc822` kullanır. Dosya indirme `application/octet-stream` ve `Content-Disposition: attachment` döndürür.

## Tekrarlar, sürümler ve sayfalama

Kritik oluşturma, mesaj, birleştirme/bölme ve CAB işlemleri `Idempotency-Key` ister. Anahtar 1–128 görünür ASCII karakter olmalıdır. Aynı kullanıcı/işlem/anahtar ve içerik aynı sonucu döndürür; farklı içerik 409 üretir. E-postada bunun yerine Message-ID ve ham içerik özeti kullanılır. Durum değişiklikleri ve bağımlılık ekleme/silme kendi hedef durumları üzerinden tekrarlanabilir çalışır.

Güncellemelerde güncel `version` gönderilir. İki düzenleyiciden biri kaydettikten sonra diğerinin eski sürümü 409 döndürür. Varlık ve makale PUT istekleri tam içerik gönderir; uzman profili PUT isteğinde atlanan boolean alanlar false olur.

Sayfalı yanıt `{ "items": [...], "next_cursor": "123" }` biçimindedir. İlk istekte `before` gönderilmez. Sonraki istekte `before=123` kullanılır. `limit` varsayılan 20, en fazla 100'dür. Boş `next_cursor` son sayfayı gösterir. Yetki/filtre koşulları sayfa sınırından önce uygulanır; kayıtlar ID azalan sıradadır.

## Doğrulama ve değişiklikler

```bash
make verify-openapi
make test
```

İlk komut geçici Python Docker konteynerinde `openapi-spec-validator==0.7.2` ile sözleşmeyi doğrular; ilk çalışmada paket indirmek için internet gerekir. Uygulama bağımlılıklarına paket eklemez. CI aynı doğrulamayı çalıştırır.

Go HTTP testleri sözleşmenin JSON olarak oturumsuz sunulduğunu ve tüm gerçek router kayıtlarının sözleşmede bulunduğunu denetler. Döngüyle kaydedilen merge/split ve PUT/DELETE bağımlılık yolları da kapsanır. Yeni bir uç nokta eklenirse sözleşme, istek örnekleri ve ilgili rol testleri birlikte güncellenmelidir. Yol eşleşme testi, bütün alanların veya iş kurallarının doğruluğunu tek başına kanıtlamaz; bunlar modül ve PostgreSQL testleriyle doğrulanır.
