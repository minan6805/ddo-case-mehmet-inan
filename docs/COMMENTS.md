# Talep mesajları ve iç notlar

Çalışan/varlık yöneticisi yalnızca kendi talebindeki `external` mesajları okur ve bu görünürlükte yazar. Destek uzmanı, lider ve admin ortak destek kuyruğundaki `external` ve `internal` kayıtları okur/yazar. “External” internetten herkese açık demek değildir: talep eden ve yetkili destek ekibi demektir. İç notların gövdesi ve sayısı çalışan yanıtına dahil edilmez; kimlik sıra aralıkları sistem genelindeki kayıt sayılarını temsil edebilir.

- `GET /tickets/{id}/comments?limit=20&before=...`: azalan ID ile sayfa, görünürlük filtresi LIMIT'ten önce uygulanır; `items` ve `next_cursor` döner.
- `POST /tickets/{id}/comments`: JSON `body`, `visibility`; zorunlu `Idempotency-Key`. Yazar kimliği oturumdan alınır; istemciden author_id kabul edilmez.
- Mesaj 1–10000 Unicode karakterdir; baş/son boşluklar temizlenir. Geçersiz görünürlük, bozuk UTF-8 ve NUL reddedilir. HTTP JSON sınırı 64 KiB.
- Tekrar anahtarı kullanıcı + talep + işlem bazında tutulur; içerik veya görünürlük değişirse 409. Başarılı kayıt sonradan talep kapansa bile aynı anahtarla yeniden alınabilir. Yeni anahtar kapalı talepte 409 verir.

Yorum yazma transaction'ı talebi satır kilidiyle alır. Böylece aynı talebin kapatılması ve yorum yazılması sıraya girer. Yorum, tekrar yanıtı, updated_at ve audit birlikte commit olur. Hata varsa tümü geri alınır. Bu işlem atama veya SLA değiştirmediğinden bunların kilitlerini almaz; sonradan bu işlemler eklenirse mevcut kilit sırası korunmalıdır.

Yorumlar uygulama üzerinden düzenlenmez/silinmez; DB trigger UPDATE/DELETE/TRUNCATE'i reddeder. Audit yalnızca yorum kimliği, talep kimliği ve görünürlük içerir; özel metin tekrar kopyalanmaz. Bu, veritabanı yöneticisine karşı kriptografik koruma değildir. Kullanıcılara genel audit okuma API'si sunulmaz.

Arayüz React metin düğümleri kullanır; yorum içeriği HTML olarak yorumlanmaz. Liste elle yenilenir; otomatik canlı mesajlaşma ve e-posta bildirimi yoktur. Çözülmüş talebe mesaj eklenebilir, kapalı talebe eklenemez. Hiçbir yorum otomatik yeniden açma veya bekleme sayacını sürdürme işlemi yapmaz. SLA'nın ilk yanıtı mevcut “İşlemde” kabulüdür; gerçek e-posta teslimi ölçülmez.

24 Ekim 2026: yorumlar için 9 gerçek PostgreSQL senaryosu, HTTP yetki/girdi testleri, canlı `make verify-comments` ve Chromium'da iki hesapla görünürlük/düz metin kontrolü başarılı. Sekiz eşzamanlı aynı gönderim tek mesaj ve tek audit bırakır. Testler geçici ayrı PostgreSQL şemasında çalışır; demo sıfırlanmaz.

T-3 e-posta, ekler ve birleştirme/bölme de tamamlandı: [ayrıntılar](EMAIL.md). Yorumun özgün satırı değişmez; birleştirme/bölmede güncel talep `comment_locations` üzerinden belirlenir. Hem liste hem ek indirme bu güncel konuma göre sahiplik ve görünürlük uygular.
