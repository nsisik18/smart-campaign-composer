# DECISIONS

Önemli mimari kararlar ADR formatında (Bağlam, Karar, Alternatifler, Sonuçlar). En altta, karar anında yazılmış gün gün uygulama notları var; ADR'ler onların özetidir. Plan: [BLUEPRINT.md](BLUEPRINT.md).

**Tek cümle:** Yedi günlük bir case'te dağıtık altyapı yerine doğruluğu göstermeyi seçtim: LLM sınırında doğrulama, onayda atomik snapshot, gönderimde lease + fencing, idempotency ve kanal başına hız sınırı.

---

## ADR-1: Modular monolith, mikroservis değil

- **Bağlam:** API, worker, migration ve mock provider farklı yaşam döngülerine sahip ama aynı domain'i paylaşıyor. Süre 7 gün.
- **Karar:** Tek Go modülü, dört binary (`api`, `worker`, `migrate`, `mockprovider`). Paketler katmanlı: handler → servis → domain/validator → repository/adapter. Dış bağımlılıklar (LLM, provider, saat, DB) interface arkasında.
- **Alternatifler:** Ayrı servisler + gRPC/HTTP sözleşmeleri; tek binary.
- **Sonuçlar:** Domain tipleri (segment planı, içerik kuralları, idempotency anahtarı) tek yerde ve derleyici tarafından tutarlı tutuluyor. Worker ayrı süreç, ayrı ölçeklenebilir. Mock provider bizim DB'ye dokunmuyor (gerçek dış servis gibi).

## ADR-2: Kuyruk olarak PostgreSQL (`FOR UPDATE SKIP LOCKED` + lease)

- **Bağlam:** Onayda alıcı listesi ve kuyruk aynı transaction'da yazılmalı; teslimat durumu incelenebilir olmalı.
- **Karar:** `deliveries` tablosu kuyruktur. Claim kısa bir `UPDATE ... FROM (SELECT ... FOR UPDATE SKIP LOCKED)`; sahiplik kilitle değil `lease_until` ile (30 s). HTTP sırasında transaction açık tutulmaz. Zamanlar DB saatiyle.
- **Alternatifler:** RabbitMQ/Redis Streams/Kafka (ek altyapı; onay ile kuyruğa alma arasında iki sistemli tutarlılık problemi: outbox gerekir).
- **Sonuçlar:** Onay atomik (kampanya durumu + teslimatlar tek commit). Kuyruk SQL ile sorgulanabilir; pano aynı tablodan okur. Bedeli: yüksek hacimde DB yükü; bu ölçekte (binlerce satır, 100/s) önemsiz. Q-01 testi eşzamanlı claim'lerin ayrık olduğunu kanıtlar.

## ADR-3: LLM çıktısı güvenilmeyen girdidir: structured output + Go'da semantik doğrulama

- **Bağlam:** Case'in en önemli kısmı Claude çıktısının güvenilir hale getirilmesi. Şema yapıyı garanti eder ama iş kurallarını (aralıklar, render sonrası uzunluk, şehir listesi) etmez; API `maxLength`/`minimum` desteklemez.
- **Karar:** `output_config.format = json_schema` (tüm objelerde `additionalProperties:false`). Ardından Go'da katı JSON çözümü (`DisallowUnknownFields`) + allowlist validator. Güvenlik sınırı prompt değil validator. Brief prompt'ta `<brief>` içinde, talimat değil veri olarak. Müşteri PII'ı Claude'a gitmez (yalnızca brief, segment özeti ve render sınırının uzunlukları).
- **Alternatifler:** Yalnızca prompt ile JSON istemek; tool use ile JSON; LLM'in SQL üretmesi.
- **Sonuçlar:** Claude hiçbir zaman SQL çalıştırmaz: compiler sabit SQL parçaları + parametrelerden oluşur, Claude'dan gelen hiçbir string SQL metnine girmez (AI-02, injection testleri). Aynı validator insan düzenlemesine de uygulanır.

## ADR-4: Allowlist segment DSL ve `unsupported_criteria`

- **Bağlam:** "VIP müşteriler" gibi veride karşılığı olmayan kriterlerde model alan uydurabilir.
- **Karar:** 5 alan, alan başına izinli operatörler, AND ile birleşen en fazla 10 kural. İfade edilemeyen kriter `unsupported_criteria`'ya yazılır ve UI'da sarı uyarı olarak gösterilir. Boş kural listesi (= herkes) reddedilir. Aynı alanın tekrarı yalnızca geçerli bir aralık olarak (gte+lte, gt+lte).
- **Alternatifler:** Serbest SQL/WHERE; OR ve iç içe yapı.
- **Sonuçlar:** Halüsinasyon yerine açık beyan. "Kuralları yok say, herkese gönder" türü bir brief'e uyan çıktı validator'da durur (AI-03). Bedeli: OR/iç içe segment yok (Known limitations).

## ADR-5: Tek onarım denemesi, sessiz kırpma yok

- **Bağlam:** Geçersiz çıktıda kullanıcıyı hemen hataya düşürmek kötü UX; sınırsız yeniden deneme maliyetli ve öngörülemez.
- **Karar:** Doğrulama hatasında TEK onarım çağrısı: önceki çıktı + makine tarafından üretilmiş alan bazlı hata listesi. İkinci çıktı da geçersizse alan bazlı hata gösterilir, düzenleme açılır. Uzun metin asla sessizce kırpılmaz. Taşıma hataları (timeout, 5xx, `max_tokens`, `refusal`) onarılmaz.
- **Alternatifler:** N deneme; otomatik kırpma; onarımsız hata.
- **Sonuçlar:** AI-04/AI-05 testleri. Onarım `llm_repairs_total` ve `llm_invalid_output_total{attempt}` ile ölçülür.

## ADR-6: Düz içerik şeması ve artımlı JSON parser ile gerçek streaming

- **Bağlam:** İçerik ekrana akmalı, ama structured output'un text delta'ları ham JSON parçalarıdır.
- **Karar:** İçerik şeması bilerek düz (3 string alan). Go'da küçük bir durum makinesi (`llm.FieldStream`) parçalardan alan bazlı çözülmüş metni çıkarır (escape, `\u`, surrogate çifti, çok baytlı UTF-8 parça sınırları). Parser yalnızca önizleme içindir; karar tam metnin katı doğrulamasıyla verilir.
- **Alternatifler:** Regex ile yarım JSON ayıklamak; streaming'siz üretim; ham JSON'u istemciye akıtmak.
- **Sonuçlar:** AI-08: her byte pozisyonunda bölme + 500 rastgele bölme, sonuç `encoding/json` ile birebir. Testler eşsiz surrogate ardından gelen escape hatasını yakaladı.

## ADR-7: SSE, WebSocket değil

- **Bağlam:** İki akış da tek yönlü (sunucu → istemci).
- **Karar:** Üretim: SSE gövdeli `POST` + `fetch` stream (yan etkili işlem, EventSource POST desteklemez). Pano: `GET` + `EventSource` (otomatik yeniden bağlanma); her olay tam durum. 10/15 s keepalive yorumu, nginx'te buffering kapalı.
- **Alternatifler:** WebSocket; polling.
- **Sonuçlar:** Ek protokol/kütüphane yok. Pano için bağlı istemci başına 500 ms'de bir DB sorgusu; çok izleyici için yayıncı gerekir (Known limitations).

## ADR-8: Tek worker replica, burst 1 limiter + %3 pay, eşzamanlılık 50

- **Bağlam:** Kanal başına en fazla 100 mesaj/s.
- **Karar:** Kanal başına ayrı dispatcher: `rate.NewLimiter(97, 1)` (100 × (1 − 0.03)) ve 50'lik semafor. Limiter HTTP çağrısından hemen önce beklenir. Claim yalnızca boş slot kadar iş alır. Tek worker replica.
- **Hesap:** Ulaşılabilir hız ≈ eşzamanlılık / gecikme. 500 ms provider gecikmesinde 100/s için ≥ 50 eşzamanlı istek gerekir; daha hızlı provider'da limiter keser.
- **Neden burst 1:** Büyük burst'lü limiter boşta biriken token'larla 1 s'de 100'ü aşabilir.
- **Neden %3 pay:** Uçtan uca testte pay olmadan dışarıdan tutarlı olarak 101/s ölçüldü: 101 başlangıç tam 1 s'ye yayılır ve goroutine uyanma gecikmesi bunları aynı pencereye düşürür. Payla gözlenen en yüksek 98/s.
- **Alternatifler:** Dağıtık limiter (Redis); provider'ın 429'una güvenmek.
- **Sonuçlar:** RATE-01 sahte saatle; uçtan uca testte ve compose'ta provider tarafında ölçülen en yüksek 98/s, provider eşik reddi 0. İki replica claim açısından güvenli ama hız 2x olur (Known limitations).

## ADR-9: At-least-once deneme + provider idempotency; exactly-once iddiası yok

- **Bağlam:** "Aynı kişiye aynı mesaj iki kez gitmez." Ağ üzerinden exactly-once HTTP çağrısı mümkün değil: provider kabul edip cevap kaybolabilir.
- **Karar:**
  - `uq_delivery (campaign_id, customer_id, channel)` aynı işin iki kez oluşmasını yapısal olarak engeller.
  - `idempotency_key = sha256(campaign:customer:channel)`; her denemede aynı anahtar gider. Provider anahtarı daha önce kabul ettiyse yeni gönderim yapmadan aynı makbuzu döner.
  - Fencing: sonuç `WHERE status='in_flight' AND attempts=$n` ile yazılır; lease'i dolmuş eski sahip yenisinin sonucunu ezemez.
- **Alternatifler:** Exactly-once iddiası; outbox + dedup tablosu bizde.
- **Sonuçlar:**
  - **Q-06 testi:** Provider kabul etti, worker DB'ye yazamadan öldü. Lease dolunca başka bir worker aynı anahtarla tekrar gönderdi. Sonuç: 2 çağrı, 1 kabul, aynı makbuz; eski sahibin geç yazması reddedildi.
  - **Canlı:** Gönderim ortasında `SIGKILL` sonrası provider kabul sayısı DB'deki sent sayısıyla birebir eşit.
  - **Sınır:** Gerçek provider idempotency key desteklemiyorsa bu garanti otomatik sağlanamaz.

## ADR-10: 429 deneme sayılır

- **Bağlam:** 429'u "bedava" saymak, sürekli 429 dönen bir provider'da sonsuz döngü demek.
- **Karar:** Her claim `attempts`'i artırır; 429 da hak tüketir (5). `Retry-After` 0-60 s'ye sınırlanarak uygulanır; yoksa full jitter backoff `rand(0, min(30 s, 1 s · 2^n))`.
- **Sonuçlar:** Deterministik profilde 429 yalnızca ilk denemede geldiği için hak tüketmesi sorun değil; bizim limiter doğru çalıştığı sürece provider eşik 429'u hiç oluşmuyor.

## ADR-11: Render sonrası rune bazlı limit, düz metin email

- **Bağlam:** "SMS en fazla 160, konu en fazla 80 karakter" ve `{{first_name}}` gibi değişkenler.
- **Karar:** Limitler render edilmiş haliyle ve Unicode code point (rune) olarak ölçülür. Üretim ve düzenlemede kitledeki en uzun ad ve şehir ile kontrol edilir; bu her alıcıyı render etmeye eşdeğer değil, onun bir **üst sınırıdır** (ad ve şehir farklı müşterilere ait olabilir; uzunluk monoton olduğu için güvenli). Onay transaction'ında her alıcı ayrıca render edilip ölçülür; tek ihlal tüm onayı geri alır. Placeholder allowlist (`{{first_name}}`, `{{city}}`), düz string replace, template engine yok. Email gövdesi `text/plain`.
- **GSM-7/UCS-2 notu:** Gerçek dünyada Türkçe karakterler tek SMS'i 70 karaktere düşürür. Case "160 karakter" dediği için 160 uygulanıyor.
- **Sonuçlar:** AI-06/AI-07. Nadiren gerçekte sığacak bir şablon reddedilebilir; güvenli taraf bu.

## ADR-12: Tek "Onayla ve gönder" aksiyonu, onay anında değişmez snapshot

- **Bağlam:** Case akışında onay ve gönderim art arda.
- **Karar:** Tek aksiyon + onay diyaloğu (alıcı sayıları, örnek render). Onay transaction'ı:
  1. Kampanyayı `FOR UPDATE` ile kilitler.
  2. `expected_version`'ı kontrol eder.
  3. Alıcıları aynı transaction'da okur.
  4. Her alıcıyı render edip ölçer.
  5. Teslimatları tek `INSERT ... SELECT unnest(...)` ile yazar.
  6. Kampanyayı `sending`'e alır.

  Onaydan sonra içerik, segment ve üretim kilitlidir. Kaydedilmemiş düzenlemeyle onay yoktur.
- **Alternatifler:** Ayrı Approve/Send; onayda yalnızca durum değişikliği + gönderim anında alıcı hesaplama.
- **Sonuçlar:**
  - **APP-01:** 5 eşzamanlı onaydan tam biri başarılı; teslimat sayısı değişmiyor.
  - **APP-02:** Onaydan sonra düzenleme 409.
  - **İzin yarışı:** Onay ile gönderim arasında kapanan izin, gönderimden hemen önceki tekrar kontrolle daraltılıyor (AUD-02).

## ADR-13: Davranış verisi denormalize

- **Bağlam:** Segment alanları son sepet, son alışveriş ve sipariş sayısı.
- **Karar:** `customers` üzerinde `last_cart_at`, `last_purchase_at`, `total_orders`. Event tablosu yok.
- **Alternatifler:** Event tablosu + agregasyon.
- **Sonuçlar:** Segment sorgusu tek tablo + index. Seed deterministik ve zamanlar seed anına göre ofset; örnek segment tam 450 kişi döner, kohort dışı İstanbullular örnek segmente bilerek uymaz.

## ADR-14: Optimistic lock (`campaigns.version`)

- **Bağlam:** Analiz ve üretim saniyeler süren LLM çağrıları; bu sürede satır kilitlemek kötü. Aynı kampanya iki sekmede açık olabilir.
- **Karar:**
  - Her yazma `WHERE version = $expected`; uyuşmazlık 409 ve güncel durum döner.
  - Üretim başında versiyon ayrılır, sonuç yalnızca o versiyonla kaydedilir.
  - Onay ve düzenleme istemciden `expected_version` alır.
- **Sonuçlar:** Eski bir stream'in sonucu yeni bir üretimin üzerine yazamaz (`superseded`). Eski görünümle onay verilemez.

## ADR-15: Testler gerçek API key olmadan, gerçek Postgres'e karşı

- **Bağlam:** Case mock LLM ile test istiyor; kuyruk ve onay doğruluğu gerçek veritabanı davranışına (kilit, SKIP LOCKED, constraint) bağlı.
- **Karar:**
  - `LLMProvider` interface'inin deterministik mock'u fixture'larla çalışır (`[[fixture:..]]`, `[[copy:..]]`).
  - Anthropic istemcisi, Claude API'yi taklit eden yerel bir HTTP sunucusuna karşı test edilir; istek şekli ve stream formatı doğrulanır.
  - Integration testleri her test için ayrı geçici Postgres veritabanı açar.
  - `make test` her şeyi container'da çalıştırır; host'ta yalnızca Docker gerekir.
- **Sonuçlar:** Testler paralel paketlerde birbirini etkilemiyor. Gerçek Claude ile davranış `make probe` ile ayrıca doğrulanır.

## Known limitations

- **Gerçek Claude:** Gerçek modelle uçtan uca doğrulama `make probe` ve `LLM_PROVIDER=anthropic` ile yapılmalı; çıktı kalitesi değişkendir. Mock'un anahtar kelime sezgisi bilerek basit.
- **Refusal fallback:** `claude-sonnet-5-5` reddettiğinde başka modele otomatik düşme (server-side fallback, beta endpoint) eklenmedi; ret kullanıcıya net hata olarak dönüyor.
- **Tek worker replica:** Hız sınırı süreç içi; çoklu replica için dağıtık limiter gerekir.
- **Pano:** Bağlı istemci başına DB sorgusu; çok izleyicide kampanya başına tek yayıncı ya da LISTEN/NOTIFY gerekir.
- **Provider idempotency:** Gerçek bir sağlayıcı idempotency key desteklemiyorsa duplicate koruması zayıflar.
- **İzin yarışı:** Onay ile gönderim arasındaki izin değişimi yalnızca gönderim öncesi kontrolle daraltılıyor; kontrol ile HTTP çağrısı arasındaki milisaniyelik pencere kapalı değil.
- **SMS uzunluğu:** GSM-7/UCS-2 (Türkçe karakterde 70) uygulanmıyor; segmentli SMS yok.
- **Segment DSL:** Auth, multi-tenancy, OR ve iç içe segment yok.
- **Bonuslar:** Zamanlanmış gönderim, A/B, kullanıcının diline göre içerik yapılmadı.

---

## Uygulama notları (gün gün)

Karar anında yazılan notlar; bazıları yukarıdaki ADR'lerde özetlendi.

#### Gün 1 (iskelet, seed, health)

- **api'nin readiness'i yalnızca DB'ye bakar** (BLUEPRINT §8'den sapma). Provider'a yalnızca worker konuşur; provider düşünce api'yi "not ready" saymak brief/analiz/üretim akışını gereksiz keser. Worker'ın `/health/ready`'si DB + provider'a bakar.
- **Seed zamanları mutlak değil, seed anına göre ofset.** Aksi halde "son 30 gün" kohortu takvim ilerledikçe boşalır. Testler sabit anchor kullanır.
- **Garanti kohort tam 450 ve sızıntısız.** Kohort dışı İstanbullular örnek segmente bilerek uymaz (sepet ≥ 32 gün önce, sepet yok veya sepetten sonra alışveriş). Integration testi sayıyı kesin bilir.
- **Mock provider aynı anahtar + farklı body'yi 422 ile reddeder.** Onaydan sonra içerik değişmediği için bu durum bizim tarafta bir bug demektir; sessizce eski makbuzu dönmek bug'ı gizlerdi.
- **Status kolonlarına CHECK constraint** eklendi (BLUEPRINT şemasında yoktu): yazım hatalı bir durum DB seviyesinde reddedilir.
- **Test DB'si ayrı compose servisi** (`db-test`, profil `test`, tmpfs). Uygulama verisine dokunmaz; testcontainers yerine seçildi çünkü Docker zaten şart ve ek bağımlılık getirmiyor.

### Gün 2 notları (segment)

- **Boş kural listesi reddedilir.** "Tüm müşteriler" segmenti v1'de desteklenmiyor; böylece "kuralları yok say, herkese gönder" türü bir brief'e uyan çıktı validator'da durur (AI-03). Prompt da bu durumda boş kural + `unsupported_criteria` üretmesini söylüyor; backend bunu yine reddeder ve alan bazlı hata gösterir.
- **Aynı alanın tekrarı yalnızca aralık olarak.** `total_orders` için gte+lte, `purchased_within_days` için gt+lte; boş aralık reddedilir. Diğer her tekrar çelişki sayılır.
- **`purchased_within_days gt N` = "son N günde almamış, hiç almamış dahil".** Pazarlamacının "son 90 gündür alışveriş yapmayan" ifadesinin doğal anlamı bu; NULL dışlansaydı hiç almamışlar sessizce kaybolurdu. Prompt ve compiler aynı tanımı kullanır.
- **Onarım yalnızca doğrulama hatasında.** Timeout, 5xx, `max_tokens` ve `refusal` onarılmaz: SDK zaten tek retry yaptı, kesilmiş çıktıyı "düzeltmek" anlamsız. Onarım çağrısı önceki çıktıyı assistant mesajı, hata listesini user mesajı olarak gönderir.
- **Analiz sırasında satır kilitlenmez.** LLM çağrısı saniyeler sürer; `FOR UPDATE` yerine yazma anında `WHERE version = $expected` (optimistic lock). Arada kampanya değiştiyse 409.
- **DB'den okunan segment de validator'dan geçer.** JSONB bizim yazdığımız normalize plan olsa da tipli hale getirmek ve şema kaymasını yakalamak için aynı yol.
- **Testler için her pakete ayrı geçici veritabanı** (`internal/testdb`). `go test` paketleri paralel çalıştırır; ortak DB'de seed'ler birbirini truncate ederdi.
- **Segment çağrısı streaming değil.** Segment kısa, kullanıcıya akıtılacak metin yok; streaming yalnızca içerik üretiminde (Gün 3).
- **`CLAUDE_EFFORT=low` varsayılan.** Segment çıkarma kısa bir yapılandırma görevi; gerçek Claude ile denendikten sonra gerekirse yükseltilecek.

### Gün 3 notları (içerik + streaming)

- **Render kontrolü bir üst sınırdır, "eşdeğer" değil** (BLUEPRINT §4.4 düzeltmesi). En uzun ad ve en uzun şehir farklı müşterilere ait olabilir; ikisiyle birlikte render etmek gerçek en kötü durumun üst sınırını verir. Uzunluk her placeholder değerinde monoton arttığı için bu sınırı geçen şablon hiçbir alıcıda limiti aşmaz. Bedeli: nadiren, aslında sığacak bir şablon reddedilebilir. Güvenli taraf bu; onay anında her alıcı ayrıca render edilip kontrol edilecek (Gün 4).
- **Modele render sınırının UZUNLUKLARI verilir**, adların kendisi değil. Böylece model payı kendisi hesaplar ve PII gitmez.
- **Artımlı parser yalnızca önizleme içindir.** Otoriter içerik, stream bitince tam metnin `json.Unmarshal` + `content.Validate` ile doğrulanmış halidir. "validated" olayı stream edilen metni backend'in normalize ettiği içerikle değiştirir.
- **Üretim başında versiyon ayrılır** (`BeginGeneration`), kayıt yalnızca o versiyonla yapılır. İki sekmeden üretim ya da üretim sırasında yeniden analiz: eski stream `failed {code: superseded}` ile biter ve hiçbir şeyin üzerine yazmaz.
- **Kısmi metin stream edilir ama asla kaydedilmez.** Kesilme (`max_tokens`) ve timeout'ta kullanıcı ne geldiğini görür, kampanya `analyzed`'da kalır ve onay kapalıdır.
- **Onarım olunca alanlar sıfırlanır** ve onarım çağrısı baştan stream edilir; ilk taslağın hataları sarı kutuda görünür kalır.
- **Placeholder yazımı katı.** `{{ first_name }}` (boşluklu) veya `{{First_Name}}` reddedilir: render düz string replace olduğu için müşteriye ham placeholder gitmesin. Repair bunu düzeltir.
- **Dış boşluklar kırpılır, içerik kısaltılmaz.** Trim zararsız bir normalizasyon; uzun metin her zaman hata olur.
- **SSE keepalive (10 s).** Sonnet 5.5'te thinking kapatılamıyor; ilk metinden önceki sessizlikte proxy'lerin bağlantıyı kesmemesi için `: ping` yorumu yazılır. İlk metin gecikmesi `first_text_ms` olarak loglanır.
- **Üretim `POST` + `fetch` stream** (EventSource değil): EventSource POST desteklemez ve üretim bir yan etki. Dashboard (Gün 6) GET + EventSource kullanacak.

### Gün 4 notları (düzenleme + onay)

- **İnsan düzenlemesi Claude çıktısıyla aynı validator'dan geçer** (`content.Validate`, aynı bounds). Geçersiz düzenleme kaydedilmez (422, alan bazlı hata); taslak frontend'de kalır. Frontend'deki kontrol yalnızca anlık geri bildirim, otorite backend.
- **Kaydedilmemiş değişiklikle onay yok.** Onay her zaman DB'deki, kullanıcının gördüğü versiyondaki içeriği gönderir (`expected_version`). Ekrandaki taslakla DB'deki içeriğin ayrışması mümkün değil.
- **Onay transaction'ında alıcı bazlı kesin kontrol.** Üretimdeki "en uzun ad + en uzun şehir" üst sınırının üstüne, onayda her alıcı tek tek render edilip ölçülür. Tek ihlal tüm onayı geri alır; kısmi kuyruk olmaz.
- **Alıcılar onay transaction'ı içinde okunur** (audience repository pool ve tx ile çalışır). Kampanya kilidi, alıcı listesi ve teslimat INSERT'i aynı anlık görüntüde.
- **Teslimatlar tek `INSERT ... SELECT unnest(...)`** ile yazılır (binlerce satır, tek round-trip). `ON CONFLICT DO NOTHING` ikinci savunma; eklenen satır sayısı beklenenden azsa onay geri alınır (sessizce eksik kuyruk yok).
- **Onay doğrudan `sending`'e geçer** (BLUEPRINT §3.1, tek "Onayla ve gönder" aksiyonu). `approved` durumu şemada kalıyor ama kullanılmıyor.
- **Idempotency anahtarı `internal/delivery`'de** ve formatı bir testle sabitlendi (bağımsız `shasum` ile hesaplanan değer): çalışan bir kampanyanın ortasında anahtar formatının değişmesi duplicate gönderim demek.
- **Mock LLM şehir ekini tanır** ("İstanbullulara"). Öncesinde kesme işaretsiz yazımda şehir kuralı düşüyor ve 450 yerine 971 kişi seçiliyordu; gerçek Claude'da sorun yok, ama demo yanıltmasın.

### Gün 5 notları (worker: kuyruk, retry, idempotency)

- **Garanti: at-least-once deneme + provider tarafında idempotent etki.** Exactly-once HTTP çağrısı iddia edilmiyor. Aynı teslimatın her denemesi aynı `Idempotency-Key` ile gider; provider anahtarı daha önce kabul ettiyse yeni gönderim yapmadan aynı makbuzu döner.
- **Sahiplik kilitle değil lease ile.** Claim kısa bir transaction'dır (`FOR UPDATE SKIP LOCKED` + `UPDATE ... RETURNING`); HTTP çağrısı sırasında transaction açık tutulmaz. Lease 30 s, provider timeout 5 s (config 3x'ten kısa lease'i reddeder).
- **Fencing token = `attempts`.** Sonuç `WHERE id AND status='in_flight' AND attempts=$n` ile yazılır. Lease'i dolmuş yavaş bir worker geç dönerse 0 satır etkiler (`ErrLeaseLost`), deneme kaydı da geri alınır. Eski sahip yenisinin sonucunu ezemez.
- **Deneme claim anında sayılır.** Gönderip ölen worker'ın denemesi de sayılmış olur; sonsuz yeniden claim döngüsü `MaxAttempts` (5) ile sınırlı. 429 da hak tüketir.
- **Retry kararları saf fonksiyon** (`Policy.Classify`): 2xx sent; 429 Retry-After (0-60 s'ye sınırlı) yoksa backoff; 5xx/timeout/bağlantı backoff; 400/422 ve diğer 4xx kalıcı. 2xx ama makbuz yoksa "kabul edildi" varsayılmaz, aynı anahtarla tekrar sorulur. Backoff: `rand(0, min(30 s, 1 s · 2^attempt))` (full jitter).
- **Zamanlar DB saatiyle** (`now()`): lease ve `next_attempt_at` worker ile DB arasındaki saat kaymasından etkilenmez.
- **Hız sınırı HTTP çağrısından hemen önce.** İlk sürümde limiter goroutine başlamadan bekleniyordu; aradaki izin kontrolü sorgusunun değişken gecikmesi başlangıçları yığdı ve uçtan uca testte 1 s'de 105 başlangıç ölçüldü. Limiter isteğin gerçekten başladığı ana taşındı.
- **%3 hız güvenlik payı (`DELIVERY_RATE_MARGIN`).** Burst 1 limiter başlangıçları tam 10 ms aralıkla verir; 101 başlangıç tam 1 s'ye yayılır. Goroutine'in uyanması mikro saniyeler gecikse dışarıdan ölçen biri aynı pencerede 101 görür (testte tutarlı biçimde ölçüldü). Limiter 97/s çalışır: pencerede en fazla 98, ~20 ms oynama toleransı; bedeli %3 verim. Sözleşme değeri (100) config'de ayrı kalır.
- **Eşzamanlılık 50, claim boş slot kadar.** Ulaşılabilir hız ≈ eşzamanlılık / gecikme: 500 ms gecikmede 100/s için ≥ 50 eşzamanlı istek. Claim yalnızca semaforda boş slot kadar iş alır; semafor beklerken lease'i tükenen iş olmaz.
- **İzin gönderimden hemen önce tekrar okunur** (AUD-02). Kapandıysa `skipped`, provider çağrılmaz. İzin okunamazsa gönderilmez; satır lease dolunca tekrar denenir.
- **Shutdown:** yeni claim durur, limiter bekleyen işler bırakılır (lease dolunca başkası alır), başlamış HTTP istekleri tamamlanır (yarıda kesmek sonucu belirsiz bırakır).
- **Kampanya kapanışı ayrı bir 1 s'lik döngü** (`FinalizeCampaigns`, tek idempotent UPDATE): her batch sonrası kontrol etmek yerine, çok worker'da da güvenli ve ucuz.
- **Tek worker replica.** Claim çoklu replica'da da güvenli, ama hız sınırı süreç içi: iki replica kanal başına 2x hız demek. Çoklu replica için dağıtık limiter gerekir (Known limitations).
- **Mock provider kendi 110/s eşiğini uygular** ve `/stats`'ta kanal başına gözlenen en yüksek hızı (`max_rps`) ve eşik reddini (`rate_limited`) raporlar: limiter'ın dışarıdan kanıtı.

**Canlı doğrulama (compose, deterministik profil):** 2.070 teslimat ~17 s'de bitti; provider kabul 2.028 = DB sent 2.028; 49 geç kabul → 49 timeout → 49 aynı anahtarla tekrar → 49 makbuz tekrarı; kanal başına en yüksek 98/s; eşik reddi 0. Worker gönderim ortasında `SIGKILL` ile öldürüldü (61 satır in_flight): yeniden başlatınca lease'ler doldu, 56 satır aynı anahtarla tamamlandı; provider yeni kabul 904 = DB sent 904.

### Gün 6 notları (canlı pano)

- **Pano GET + EventSource, üretim POST + fetch stream.** EventSource yeniden bağlanmayı bedavaya verir ve pano yan etkisiz bir okuma; üretim ise POST (yan etki) olduğu için fetch ile okunuyor.
- **Her olay TAM durumdur** (`snapshot` bağlanınca, `stats` 500 ms'de bir). Delta yayınlamak yerine tam durum: kaçırılan olay veya yeniden bağlanma hiçbir şeyi bozmaz, istemcide birleştirme mantığı yok. Kampanya başına veri küçük (iki kanal, son 20 deneme).
- **Terminal durumda akışı istemci kapatır.** Sunucu son `stats`'tan sonra akışı bitirir; EventSource kapanan akışa otomatik yeniden bağlanacağı için istemci terminal durumu görünce `close()` çağırır. Terminal kampanyaya bağlanan istemci tek bir `snapshot` alır.
- **İstemci başına 500 ms'de bir DB sorgu turu.** Sorgular campaign_id index'leri üzerinde ve ucuz; demo ve birkaç izleyici için yeterli. Çok sayıda izleyici için tek bir yayıncı (kampanya başına bir sorgu, N abone) ya da LISTEN/NOTIFY gerekir (Known limitations).
- **Panoda iki hız:** "gönderim/s" (sent olan teslimatlar) ve "istek/s" (provider'a giden istekler, retry'lar dahil). İkincisi hız sınırının panodaki karşılığı; ikisi arasındaki fark retry yükünü gösterir.
- **Panoda PII yok:** son olaylar tablosunda alıcı adresi/adı değil teslimat ID'si, kanal, HTTP kodu, sonuç ve süre.
