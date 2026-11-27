# AI oturumları: özet (claude.ai planlama sohbeti ve Claude Code oturumu)

Bu dosya case boyunca kullandığım AI oturumlarının özetlenmiş halidir. Kararların tartışıldığı claude.ai planlama sohbetinin ham hali kişisel bilgiler içerdiği için paylaşılmadı; görüşmede birlikte açabilirim. Claude Code oturumunun ham export'u [claude-code-oturumu.md](claude-code-oturumu.md) dosyasında. Her adımda: ne yapıldı, AI'dan ne istedim, AI ne üretti ve ne sordu, benim kararım ne oldu, hangi alternatifler vardı ve karar nasıl doğrulandı.

"Kararım" satırları kayıtta gerçekte ne yaptığımı ve neden öyle yaptığımı yazar. Planı önceden ayrıntılı yaptığım için kodlama fazlarında kararım çoğunlukla Claude'un o fazdaki önerilerini kabul etmekti; her birini neden kabul ettiğimi ve nerede söylediğimi yapmadığımı da yazdım.

---

## 1. Case analizi, mimari araştırma ve plan (19-22 Kasım, claude.ai + Gemini + ChatGPT)

- **19-21 Kasım:** Case 19 Kasım'da geldi. İlk üç günde case'i inceledim ve en uygun mimari için araştırma yaptım. Bu kısım kayıtlı AI oturumlarının dışında; 22 Kasım'da planları karşılaştırırken bu araştırmayı temel aldım.
- **Yapılan iş:** Case'i anlamak, mimariyi belirlemek ve işi 7 faza bölen bir uygulama planı çıkarmak.
- **İstediğim:** Case'in yorumsuz bir özeti; ardından bu özeti Gemini ve ChatGPT'ye de verip üç ayrı mimari önerisi aldım ve Claude'a karşılaştırttım.
- **AI çıktısı:** Üç blueprint ve bunların eleştirisi. Gemini planında worker çökünce takılı kalan mesajlar için lease yoktu, retry için `failed` durumu kullanılıyordu, %30 hata oranlı "kayıpsız" testi istatistiksel olarak flaky olurdu. ChatGPT planında kanal başına 8 eşzamanlı istek vardı; bu ~500 ms gecikmede ~27 istek/s demek, 100/s'e hiç ulaşılamazdı.
- **Kararım:** ChatGPT planını temel aldım, hataları düzeltilmiş halini BLUEPRINT.md olarak birleştirttim. Kapsamı teslim süresine sığacak şekilde kestim: ayrı Approve/Send yerine tek "Onayla ve gönder", Playwright ve ek state kütüphanesi yok.
- **Alternatifler:** Tek modelin planıyla ilerlemek; Redis/RabbitMQ kuyruğu (Postgres `SKIP LOCKED` seçildi: ek broker yok, onay ve kuyruk aynı transaction'da).
- **Doğrulama:** Structured output'un `maxLength` desteklemediği Claude dokümanından doğrulandı; uzunluk kurallarının tamamen Go'da olması bu yüzden.

## 2. Faz 1: iskelet, seed, mock provider (22 Kasım, Claude Code)

- **İstediğim:** BLUEPRINT'e göre repo iskeleti, compose, migration, 450 kişilik garanti kohortlu seed, mock provider iskeleti, health uçları ve Claude structured output + streaming denemesi. Kuyruk, retry ve idempotency kodunu kendim yazacağımı ya da satır satır inceleyeceğimi belirttim.
- **AI çıktısı ve önerileri:** BLUEPRINT'ten beş sapma: api readiness yalnızca DB'ye bakıyor; seed zamanları seed anına göre ofset; status kolonlarına CHECK; mock provider aynı anahtar + farklı içerikte 422; testler için tmpfs `db-test`.
- **Kararım:** Beşini de kabul ettim. Readiness'ın yalnızca DB'ye bakması, worker ya da provider geçici olarak düştüğünde api'nin gereksiz yere trafikten çıkmasını engelliyor; onlar kendi health uçlarını zaten raporluyor. Seed zamanlarının seed anına göre ofset olması, demo hangi gün çalıştırılırsa çalıştırılsın "son 30 gün" kohortunun hep 450 kalmasını sağlıyor. Status kolonlarındaki CHECK, koddaki bir hatanın geçersiz bir durumu DB'ye yazmasını en alt katmanda durduruyor. Aynı anahtar + farklı içerikte 422, idempotency anahtarının yanlış kullanılmasını sessizce yutmak yerine görünür kılıyor. tmpfs `db-test`, testleri gerçek Postgres'e karşı hızlı ve izole çalıştırıyor, geliştirme verisine dokunmuyor.
- **Alternatifler:** Mutlak seed tarihleri (kohort takvim ilerledikçe boşalırdı), testcontainers (Docker zaten şart, ek bağımlılık gereksiz).
- **Doğrulama:** Seed determinizmi ve kohort = 450 testleri; `docker compose up` sonrası health ve provider smoke testi.

## 3. Faz 2: segment (22 Kasım)

- **AI çıktısı ve önerileri:** Allowlist segment DSL, validator, parametreli SQL compiler. Kararlar: boş kural listesi reddedilir ("herkese gönder" türü brief validator'da durur); "son N günde almamış" hiç almamışları da kapsar; onarım yalnızca doğrulama hatasında (timeout ve kesilmede değil); analizde satır kilidi yerine optimistic lock.
- **Kararım:** Kabul ettim. Boş kural listesini reddetmek en riskli hatayı, yani "herkese gönder"e dönüşen bir yanlış anlamayı, daha SQL'e gelmeden durduruyor. "Son N günde almamış"ın hiç almamışları da kapsaması pazarlamacının cümlesinin gündelik anlamıyla aynı; aksi hem beklentiyi hem garanti kohortu bozardı. Onarımı yalnızca doğrulama hatasında denemek, timeout ya da kesilmede aynı isteği tekrarlayıp maliyeti ikiye katlamayı önlüyor; o durumlarda modelden düzeltme istemenin anlamı yok. Analiz bir Claude çağrısı boyunca sürdüğü için satırı saniyelerce kilitli tutmak yerine optimistic lock daha basit ve güvenli.
- **Doğrulama:** Gerçek Postgres'te örnek brief = 450 kişi; 21 geçersiz çıktı türünün reddi; injection çıktıları SQL'e ulaşmıyor.

## 4. Faz 3: içerik üretimi ve streaming (22 Kasım)

- **AI çıktısı ve önerileri:** Artımlı JSON parser, versiyonlu üretim (eski stream yeni versiyonu ezemez), POST + fetch stream. BLUEPRINT'teki bir iddianın düzeltmesi: "en uzun adla render, her alıcıyı render etmeye eşdeğerdir" yanlış, bu bir üst sınır.
- **Kararım:** Kabul ettim. Artımlı parser, metnin alan alan ekrana gerçekten akmasını sağlıyor; BLUEPRINT'te sahte typing efekti değil gerçek streaming şartı vardı. Versiyonlu üretim, kullanıcı yeniden ürettiğinde eski stream'in geç gelen parçalarının yeni metni ezmesini engelliyor. POST + fetch, brief'i URL'ye koymadan stream açmanın en sade yolu (EventSource yalnızca GET destekliyor). "En uzun adla render" düzeltmesini de kabul ettim: bu kontrol bir üst sınır olduğu için kesin kontrolün onay anında alıcı bazlı yapılması gerekiyordu (ADR-11).
- **Doğrulama:** Parser her byte bölme pozisyonunda `encoding/json` ile karşılaştırıldı; render sonrası 160/161 sınır testleri.

## 5. Faz 4: düzenleme ve onay (22 Kasım)

- **AI çıktısı:** İnsan düzenlemesi Claude çıktısıyla aynı validator'dan geçiyor; onay tek transaction'da alıcı bazlı kesin kontrol yapıp teslimatları yazıyor; idempotency anahtarı formatı testle sabitlendi. Testler bir gerçek hatayı (onay ekranında tanımsız değişken) ve mock'un "İstanbullulara" yazımında 450 yerine 971 kişi seçmesini yakaladı.
- **Kararım:** İnsan düzenlemesinin aynı validator'dan geçmesini kabul ettim, çünkü limit ve placeholder kuralları metni kimin yazdığından bağımsız olmalı. Onayın tek transaction'da olması, yarım onaylanmış bir kampanya ya da onaydan sonra değişmiş bir metinle gönderim ihtimalini kaldırıyor. Faz 5 için Claude, kuyruk kodunu benim yazmamı ya da satır satır incelememi hatırlattı. Zaman baskısıyla "Sen yaz, açıklamalı" dedim; açıklamalı istememin sebebi kodu sonradan okuyup anlayabilmekti. İlk mesajımdaki sözü o anda tutmadım.
- **Doğrulama:** 5 eşzamanlı onaydan biri başarılı, teslimatlar tek set; onaydan sonra düzenleme 409.

## 6. Faz 5: worker, retry, idempotency (22 Kasım)

- **AI çıktısı ve önerileri:** Lease ile sahiplik, `attempts` ile fencing, at-least-once + provider idempotency, full jitter backoff. Uçtan uca test limiter'ın 1 s'de 105 istek başlattığını gösterdi; limiter HTTP çağrısının hemen önüne taşındı. Tam 100/s dışarıdan 101 ölçüldüğü için %3 pay önerildi. İnceleme sırası önerildi (classify → store → dispatcher → Q-06 testi).
- **Kararım:** Kabul ettim. Lease, ölen bir worker'ın aldığı işin sonsuza kadar takılı kalmamasını sağlıyor; `attempts` ile fencing, lease'i kaçırmış eski bir worker'ın sonucu yazıp yeni denemeyi ezmesini engelliyor. Dağıtık bir sistemde exactly-once garanti edilemediği için at-least-once + provider tarafında `Idempotency-Key` en dürüst çözüm: tekrar gönderim olsa bile müşteriye ikinci mesaj gitmiyor. Full jitter, 429 sonrası bütün işlerin aynı anda tekrar denenmesini dağıtıyor. Limiter'ın HTTP çağrısının hemen önüne taşınmasını ve %3 payı ölçüm gösterdiği için kabul ettim: sınır provider tarafında sayılıyor, bizim tarafımızda değil. Önerilen satır satır incelemeyi o anda yapmadım; worker'ı sonradan davranışıyla doğruladım (gönderim ortasında `SIGKILL`, gerçek Claude ile 6.165 gönderim).
- **Alternatifler:** Büyük burst'lü limiter (boşta biriken token'larla 100'ü aşardı), çoklu replica için dağıtık limiter (kapsam dışı, Known limitations).
- **Doğrulama:** Q-01..06 gerçek Postgres'e karşı; compose'ta 2.070 teslimat ve gönderim ortasında `SIGKILL`: provider kabulü = DB sent, mükerrer 0.

## 7. Faz 6-7: canlı pano, teslim hazırlığı (22 Kasım)

- **AI çıktısı:** Pano her olayda tam durumu gönderiyor (yeniden bağlanma güvenli); CI; gizli bilgi taraması. Gerçek Claude ile hiç denenmediği README'de açıkça yazıldı.
- **Kararım:** Kabul ettim. Panonun her olayda tam durumu göndermesi, bağlantı koptuğunda kaçan olayları yeniden oynatma mantığına gerek bırakmıyor; tarayıcı yeniden bağlandığında doğru durumu görüyor. Gerçek Claude ile denenmediğinin README'de açıkça yazılması, değerlendirene doğrulanmamış bir iddia sunmamak içindi. Repo için git ve commit'i başlattım; commit'in yanlış GitHub hesabıyla atılacağını fark edip doğru hesapla atılmasını sağladım.
- **Doğrulama:** CI'da frontend, backend ve compose işleri yeşil.

## 8. Kendi denemem: desteklenmeyen kriter (23 Kasım)

- **Yapılan iş:** Uygulamayı tarayıcıda kendim denedim.
- **Bulduğum:** "İstanbulkart kullananlar" gibi veride olmayan bir kriter yazınca kullanıcı anlamsız bir doğrulama hatası görüyordu ve boşuna bir onarım çağrısı yapılıyordu.
- **AI önerisi:** Ayrı bir `no_supported_criteria` hatası: desteklenmeyen kriterleri ve desteklenen alanları göster, onarım deneme.
- **Kararım:** Düzeltilmesini istedim (ADR-4).
- **Diğer:** Gerçek API testi için Groq gibi ücretsiz bir model önerdim; Claude doğrulamak istediğimiz şeyin Claude'a özgü olduğunu (structured output, streaming) söyledi, gerçek Claude'da kaldım. Gönderimde 13 başarısız teslimat görünce sordum; mock provider'ın kasıtlı kalıcı 422 profiliymiş.

## 9. Teslim öncesi denetim (25 Kasım, claude.ai)

- **İstediğim:** Repoyu teslim listesine göre denetlemek.
- **AI çıktısı:** Eksikler: repo public değil, ai-sessions boş, `make test` host'ta Go 1.27 istiyor, sistem gerçek Claude ile hiç çalışmamış.
- **Kararım:** Gerçek bir API key alıp sistemi gerçek Claude ile test etmeye karar verdim. Testlerin tekrar çalıştırılabilir script'ler olarak yazılmasını istedim.

## 10. Gerçek Claude testleri (25 Kasım)

- **Yapılan iş:** Taze clone'da `make test` ve `docker compose up`, 3 probe, 30 senaryo (injection, desteklenmeyen kriter, eşzamanlı onay ve üretim dahil), gönderim ortasında worker'a `SIGKILL`, DB tutarlılık sorguları.
- **Sonuç:** Güvenilirlik temiz: 6.165 provider kabulü = 6.165 DB sent, mükerrer 0, izin ihlali 0. Ama gerçek model, mock'un göstermediği üç hata çıkardı:
  - `{{city}}'a` → "Bursa'a": Türkçe ek ünlü uyumuna göre değişiyor.
  - "İzmir'deki VIP müşterilere": VIP kuralı uygulanamadı ama metin "VIP müşterilerimize özel" diyordu.
  - Model alanları alfabetik sırayla üretiyordu (Go `map`'i JSON'a alfabetik yazıyor).
- **Kararlarım ve alternatifler:**
  - Ek: validator kuralı + prompt (ADR-16). Şehirler için Türkçe ek tablosu reddedildi; eksiz cümle her değerde doğru ve basit.
  - VIP: uygulanamayan kriterler prompt'a ve onay diyaloğuna (ADR-17). Analizi tamamen reddetmek reddedildi; "İzmir'deki herkese" gönderim meşru bir seçim, karar insanda kalmalı.
  - Sıra: şema sıralı struct'a taşındı.
- **Doğrulama:** Gerçek Claude ile 7 brief'lik tekrar testinde üçü de 0.

## 11. Bağımsız inceleme 1 (25-26 Kasım, Claude Code)

- **İstediğim:** Düzeltmelerin, yazan oturumdan bağımsız olarak hata bulma odaklı incelenmesi.
- **Bulunan:** Türkçe ek kuralı dört mevcut testi bozmuştu, biri yanlış sebeple yeşil kalıyordu; önceki oturumun "testler yeşil" iddiası doğrulanmamıştı. README'deki `make probe` talimatı çalışmıyordu.
- **Kararım:** Düzeltmelerin commit'lenmesini onayladım. Mutation testiyle şema sırası testinin gerçekten çalıştığı doğrulandı.

## 12. Sıfırdan değerlendirme ve son düzeltmeler (26 Kasım)

- **Yapılan iş:** Repoyu önceki bağlamı bilmeyen yeni bir Claude oturumuna sıfırdan değerlendirttim. Gelen önerileri ana oturumda tek tek tartışıp karar verdim.
- **Gelen öneriler ve kararlarım:**
  - "Model %30 yerine %20 yazarsa?" sorusunun cevabı yoktu → deterministik sadakat kontrolü (ADR-18). İkinci bir LLM ile "sadakat puanı" reddedildi: yavaş, pahalı ve kendisi de doğrulanmalı.
  - Makinede Postgres çalışıyorsa `docker compose up` patlıyordu → DB portları host'a kapatıldı.
  - Key eklenince sistem sessizce mock'la çalışıyordu → key varsa otomatik gerçek Claude.
  - Yorumlarda bayat notlar ve plan atıfları vardı → temizlendi; mock provider'ın gereksiz `chaos` profili kaldırıldı.
  - Reddettiklerim: segment çiplerini düzenlenebilir yapmak (yeni UI özelliği), 429'da adaptif hız (mock için gereksiz), commit geçmişini sonradan bölmek (sahte geçmiş olurdu).
- **Doğrulama:** Gerçek Claude ile 5 sadakat senaryosu; README'ye gerçek Claude ile alınmış ekran görüntüleri.

## 13. Bağımsız inceleme 2 (26 Kasım, Claude Code)

- **Bulunan:** Sadakat kontrolünün link regex'i cümle sonundaki noktayı linke katıyordu (brief'teki linki "uydurma" sayardı); toplu yorum temizliği bir yorumu bozuk bırakmıştı.
- **Kararım:** Düzeltilip commit'lenmesini onayladım. CI yeşil.

---

## Geriye bakınca

- Planı önceden yapmak işe yaradı; çekirdek akış ve güvenilirlik testleri ilk turda sağlam çıktı.
- Zayıf yanım: kodlama fazlarında diff'leri kendim okumadım ve worker kodunu yazdırdım. Bunu teslimden önce gerçek API testleri, SIGKILL testi ve iki bağımsız incelemeyle kapatmaya çalıştım.
- Mock'la geçen testler yeterli değildi: üç ürün hatasını yalnızca gerçek model ortaya çıkardı.
- Bir dahaki sefere: her fazı ayrı commit'lemek, faz sonunda diff'i okuyup en az bir kararı sorgulamak, gerçek modeli ilk gün denemek.
