# Otomatik atama ve kapasite — F-3 / T-2

## Temel davranış

Her kategori bir veya daha fazla beceri etiketi gerektirir. Uzman bu etiketlerin tamamına sahip olmalı; atamaya açık, izinde olmayan, vardiyası açık ve kapasitesi müsait olmalıdır. Uygun adaylar arasında beceri düzeyi, boş kapasite ve dağıtım sırası birlikte puanlanır.

Açık, işlemde ve kullanıcı yanıtı bekleyen talepler yük sayılır. Çözülmüş/kapalı talepler kapasite tüketmez; atanan uzman bilgisi geçmiş için korunur. Çözülen talep yeniden açılırsa eski uzmanın kapasitesi ve uygunluğu tekrar kontrol edilir. Artık uygun değilse başka uzmana gider veya kuyruğa alınır.

Uygun uzman yoksa talep kaybolmaz ve oluşturma isteği hata vermez: `assignee_id=null`, talep “Atama bekleyenler” kuyruğundadır. Kapasite açıldığında veya vardiya başlayınca tekrar değerlendirilir. Atama talebi otomatik olarak işleme almaz; ilk SLA kabul yanıtı hâlâ personelin “İşlemde” hareketidir.

## Puanın açıklaması

Her bileşen 0–1000 arası tam sayıdır:

- Beceri: gereken etiketlerdeki toplam düzey / mümkün olan toplam düzey.
- Boş kapasite: `(kapasite - aktif yük) / kapasite`.
- Sıra: uygun adaylar en eski `last_assignment_seq` ve kullanıcı ID'sine göre sıralanır. En eski atanan en yüksek sıra puanını alır.

Toplam: `5 × beceri + 3 × boş kapasite + 2 × sıra` (0–10000). Ağırlıklar %50/%30/%20'dir. Eşit puanda daha eski atama sırası kazanır. Seçilen uzmanın üç bileşeni ve toplam puanı geçmişte saklanır. Aynı beceri/yükteki üç uzman ilk üç talebi sırayla alır; daha becerili uzmanın tercih edilmesi ve doldukça diğerine yönelme birlikte sağlanır.

Geçerli bir atama sırf başka adayın puanı arttı diye taşınmaz; her turda gereksiz devir yapılmaz. Devir, mevcut uzmanın artık uygun olmamasıyla olur.

## Kapasite yarışını nasıl önlüyoruz?

Yazma işlemi sırası: takvim paylaşımlı kilidi → atama transaction kilidi → talep/uzman satır kilitleri. Bu küçük sistemde atama kararları PostgreSQL advisory transaction lock ile sıraya alınır; aday profil satırları da kilitlenir. Yük, kapalı olmayan iş kayıtlarından sorgulanır; ayrıca seçilen uzmanın yükü yazımdan hemen önce yeniden kontrol edilir. Böylece bir önceki atama commit olmadan ikinci karar boş yeri tekrar kullanamaz.

Oluşturma, durum değiştirme, izin/vardiya/beceri/kapasite değişimi ve arka plan kontrolü aynı kilit protokolünü kullanır. Taleple atama, geçmiş ve audit aynı transaction içinde commit edilir. Doğrudan SQL ile bu protokolü atlayan bir yazıcının kapasiteyi koruduğu iddia edilmez; gelecekteki e-posta/simülatör de aynı talep servisini kullanmalı.

Bu tasarım doğruluğu ve açıklanabilirliği hedefler; tüm atama yazıcılarını sıraya aldığı için yüksek hacimde darboğaz olabilir. Çok büyük ölçek için havuz/takım bazlı kilitleme veya SERIALIZABLE + retry değerlendirilebilir.

## İzin, vardiya, beceri ve kapasite değişimi

Takım lideri/admin profili kaydeder. Uzman izne alınır veya ataması kapatılırsa bütün aktif işleri yeniden değerlendirilir. Yer yoksa ataması kaldırılıp kuyruğa girer. İzin bitince kuyruk tekrar doldurulur; başka uzmanda geçerli ataması olan iş gereksiz yere geri çekilmez.

Kapasite düşürülünce fazla işler ID sırasıyla devredilir; kalan yük yeni kapasiteyi aşmaz. Gerekli beceri kaldırılırsa ilgili işler devredilir. Vardiya bitişini arka plan kontrolü fark eder. Profil güncellemesi ve devir aynı transaction'dadır; eski sürümle kaydetme 409'dur.

Varsayılan kontrol `ASSIGNMENT_SWEEP_INTERVAL=10s`, bir tur `ASSIGNMENT_SWEEP_BATCH=100`. Eski kontrol zamanına göre tarama yapılır. Çözümde açılan yer için 20 kuyruk kaydı, profil değişiminde 100 kayıt hemen denenir; kalanlar sonraki turlara kalır. Kuyrukta en eski deneme önceliklidir; aynı yaştaki taleplerde öncelik ve oluşturma sırası kullanılır.

## Arayüz ve erişim

- Talep sahibi detayda atanan uzmanı veya uygun uzman beklendiğini görür.
- Destek ekibinde kuyruk filtreleri: tüm kuyruk, bana atananlar, atama bekleyenler.
- Uzmanlar ortak destek kuyruğunu okuyup işleyebilir. “Bana atananlar” bir görünüm filtresidir; uzmanlar arasında ayrı bir erişim sınırı değildir.
- Atama geçmişi yalnızca destek uzmanı/lider/admin tarafından okunur. Karar, eski/yeni uzman, başlatan kullanıcı, zaman ve puan bileşenleri görünür.
- Uzman panosunu destek ekibi okur; yalnızca lider/admin kapasite, beceri, izin ve vardiyayı düzenler.
- Uzmanın kendisi profili değiştiremez. Çalışan ve varlık yöneticisi uzman panosu/atama geçmişi/yeniden deneme ucuna erişemez.

## Demo ve sınırlar

`uzman@destekmasa.local`, `uzman2@destekmasa.local`, `uzman3@destekmasa.local` hesapları, ortak yerel `DemoDestek2026!` parolasıyla açılır. Kapasiteleri başlangıçta 8'dir. Demo vardiyaları her gün tüm gündür; cumartesi atama denemek mümkündür. SLA takvimi bundan bağımsızdır ve hafta sonunu saymaz. Seed, yönetici tarafından düzenlenmiş mevcut kapasite/vardiya/becerileri geri yazmaz.

Tek günlük pencere ve seçilen hafta günleri desteklenir; gece yarısını aşan vardiya, planlı tarih aralıklı izin ve çoklu takım/havuz henüz yok. İzin anlık aç/kapa ayarıdır. Beceri kataloğu ve kategori→gereken etiket eşlemesi migrasyonda tanımlıdır; uzman becerileri arayüzden düzenlenir, katalog/eşleme düzenleme ekranı yoktur. Bütün atama kararları bu sürümde tek global kilit kullanır. Atama geçmişi UPDATE/DELETE/TRUNCATE trigger'larıyla normal değişikliklere kapalıdır; DB yöneticisinin trigger'ları kaldırmasına karşı koruma iddiası yoktur.

## Kanıt

```bash
make verify-assignment  # canlı F-3 API + ayrı PostgreSQL'de T-2
make prove-assignment   # yalnızca atama PostgreSQL grubu
make verify             # mevcut API kimlikleri + tüm PostgreSQL testleri; eksikler ayrıca FAIL
```

Canlı F-3 senaryosu profil değerlerini değiştirmeden liderle kaydeder; sürüm/audit artar. Otomatik atama, sahiplik/rol sınırı, geçmiş, tekrar denemede atama sabitliği, bana atananlar ve kapasite kontrol edilir. `[Atama doğrulama]` başlıklı talep açılıp kapatılır. İzin/devir ve yarışın kapsamlı kanıtı izole PostgreSQL'dedir; normal demo verisi oradan etkilenmez.

16 PostgreSQL senaryosu: tekrar seed ve düzenlemelerin korunması; eşit adaylarda dağıtım; gerekli beceri; vardiya dışı; kapasite dolu kuyruğu; **iki eşzamanlı HTTP isteği/tek boş yer**; 24 eşzamanlı talep/toplam kapasite; izin ve devir geçmişi; yedeksiz izin/kuyruk ve dönüş; çözüm/yer açılması/beklemenin yük sayılması/yeniden açma; vardiya sınırı; kapasite düşürme; beceri kaldırma; eski sürüm/geçersiz beceri rollback; append-only geçmiş/tekrar sabitliği; kuyruk kapsamı.

24 Ekim 2026: 16 senaryo PASS, grup 2,26 s (yarış algılayıcısı açık). Çıktıdan:

```text
--- PASS: TestPostgresAssignmentEngine/two_concurrent_http_requests_cannot_take_one_slot (0.45s)
--- PASS: TestPostgresAssignmentEngine/many_concurrent_creations_preserve_total_capacity (0.18s)
--- PASS: TestPostgresAssignmentEngine/leave_reassigns_all_active_work_with_history (0.02s)
--- PASS: TestPostgresAssignmentEngine/capacity_reduction_moves_excess_work (0.02s)
```

Chromium'da otomatik atanan talep, uzmanı izne alma, devrin geçmişte görünmesi, izni geri kaldırma ve mobil profil kontrolleri PASS. Tarayıcı testi profili izin dışına geri getirir; gerçek devir olayları geçmişte kalır. F-3/T-2, tam doğrulamada PASS; T-3 e-posta ve T-4 varlık/değişiklik adımları da daha sonra tamamlandı; F-4 ve F-7 henüz uygulanmadı.
