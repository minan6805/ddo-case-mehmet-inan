# Raporlar

Rapor ekranı yalnız takım lideri ve yöneticiye açıktır. Çalışan, uzman ve varlık yöneticisi için hem ekran düğmesi gizlidir hem API 403 döner. Oturumsuz erişim 401'dir. Yanıtlar talep açıklaması, yorum veya parola içermez.

## Tarih ve kategori kapsamı

`GET /reports?from=2026-10-01&to=2026-10-25&category=network`

- Başlangıç ve bitiş **oluşturulma tarihine** uygulanır; iki tarih de İstanbul saatine göre dahildir. SQL aralığı başlangıç günü 00.00 dahil, bitişten sonraki gün 00.00 hariçtir.
- Tarihler yoksa İstanbul'da bugün dahil son 30 gün kullanılır. Tarihlerden yalnız biri verilirse, sıralama tersse, tarih geçersizse veya aralık 366 günü aşarsa 400 döner.
- Kategori isteğe bağlıdır; mevcut kategori kodu olmalıdır. Tüm dağılımlar ve SLA ölçümleri aynı talep grubunu kullanır.
- Durumlar taleplerin **şimdiki** durumudur. Bu rapor, geçmiş bir günün durumunu yeniden kurmaz ve seçilen dönemde tamamlanan bütün talepleri ölçmez.
- Günlük seri, o gün oluşturulan talep sayısıdır; talepsiz günler sıfırla bulunur.

Özet; toplam, aktif, atanmamış aktif, çözüldü ve kapalı sayılarını içerir. Aktif durumlar açık, işlemde ve yanıt bekleniyordur. Durum/öncelik/kategori dağılımlarının ve günlük serinin toplamı, toplam talebe eşittir. Birleştirilmiş kaynak kayıtlar silinmediği için kapalı talepler içinde sayılır; kayıtları tek talep gibi birleştiren ayrı bir iş hacmi ölçümü yapılmaz.

## SLA ölçümleri

SLA bölümü, bu talep grubunun kayıtlı `ticket_sla` sayaçlarını kullanır. Rapor salt okunurdur; açıldığında hesaplama veya eskalasyon çalıştırmaz. En eski ve en yeni kontrol zamanı ayrıca gösterilir. Yeni/süren taleplerin sayaçları arka plan işçisi tarafından güncellenir.

- İlk yanıt ortalaması yalnız `first_response_at` dolu kayıtların tükettiği iş dakikasıdır. İlk yanıt, işleme alma/kabul anlamındadır.
- Çözüm ortalaması yalnız `completed_at` dolu kayıtları içerir. Yeniden açılmış ve henüz çözülmemiş talepler bu örnek grubuna girmez.
- Hedef süreden önce tamamlanma oranı, ilgili tamamlanmış örnek sayısına bölünür. Motorun kuralıyla uyumlu olarak tüketim bütçeye tam eşitse ihlaldir; zamanında sayılması için küçük olmalıdır.
- Hiç örnek yoksa ortalamalar JSON'da `null`, ekranda `—` gösterilir. Eksik SLA kayıtları ayrıca sayılır.
- Uyarı, ilk yanıt/çözüm ihlali ve eskalasyon, geçmişte kaydedilmiş olaylardan her gösterge için **tekil talep** sayısıdır. Bir talebin iki sayaçta eskalasyonu olması toplamı ikiye katlamaz. Olayın tarihi değil, talebin oluşturulma tarihi filtrelenir.
- Geçmiş ihlal kaydı ile güncel sayaçtan hesaplanan tamamlanma oranı farklı ölçümlerdir; takvim düzenlemesi geçmiş olayları silmez.

## Güncel iş yükü

İş yükü bölümü **tarih ve kategori filtresinden bağımsızdır**. Her uzman profilinin o andaki aktif talep sayısını, kapasitesini, etkinlik ve izin bilgisini gösterir. Etkin profil, o anda vardiyada olduğu anlamına gelmez. Çözüldü/kapalı talepler yük tüketmez. Sıfır yüklü ve devre dışı profiller de gösterilir. Uzmanlara atanmış yük + atanmamış aktif = tüm aktif talepler.

En fazla 500 uzman profili desteklenir; aşılırsa eksik tablo yerine 409 `REPORT_LIMIT` döner.

## Tutarlılık ve doğrulama

Bütün sorgular aynı `REPEATABLE READ READ ONLY` transaction'ındadır. Eşzamanlı talep değişikliği, raporun bir bölümüne girip diğerine giremez. `snapshot_at` rapor transaction başlangıcıdır; SLA kontrol zamanı değildir. Oluşturulma tarihine göre seçim `tickets_created_category_idx` indeksini kullanabilecek şekilde yazılmıştır; performans ölçümü ayrıca yapılacaktır.

- `make verify-reports`: canlı API'de rol kontrolü, tarih/kategori doğrulaması, gerçek talep listesine karşı bağımsız sayım, dağılımlar, global yük ve boş veri; oluşturduğu örnek talep sonunda kapatılır.
- `make prove-reports`: ayrı PostgreSQL'de gün sınırları, dağılımlar, tekilleştirme, tam bütçe sınırı, ortalamalar, salt okunurluk, HTTP erişimi, eşzamanlı yazı sırasında tutarlılık ve uzman sınırı.
- `scripts/browser-reports.cjs`: Playwright ile gerçek rapor değerleri, filtreler, boş sonuç, mobil düzen, hata sonrası yeniden yükleme ve oturum sonlanması.
