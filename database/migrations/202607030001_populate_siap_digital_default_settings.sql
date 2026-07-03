-- Migration: populate_siap_digital_default_settings
-- Created at: 2026-07-03

-- Update Site Identity settings if not set or default
INSERT INTO site_settings (key, value) VALUES
('site_title', 'Siap Digital')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
WHERE site_settings.value = '' OR site_settings.value = 'NewsPaper';

INSERT INTO site_settings (key, value) VALUES
('site_tagline', 'Berita Terkini & Terpercaya')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
WHERE site_settings.value = '';

-- Populate About Page (page_about_content)
INSERT INTO site_settings (key, value) VALUES
('page_about_content', '<p class="lead"><strong>Siap Digital News</strong> adalah portal berita daring terpercaya yang dikelola oleh <strong>PT SIAP DIGITAL AGENCY</strong>. Kami berkomitmen untuk menghimpun, mengurasi, dan menyajikan informasi terkini, mendalam, dan berimbang demi memenuhi kebutuhan informasi masyarakat Indonesia secara cepat, akurat, dan transparan.</p>

<hr style="margin: 30px 0; border: 0; border-top: 1px solid var(--border);" />

<h3>Visi & Misi Perusahaan</h3>
<div style="margin: 20px 0;">
  <h4>Visi</h4>
  <p>Menjadi rujukan informasi harian yang cepat, relevan, dan mudah diakses oleh masyarakat Indonesia dari berbagai kalangan.</p>
</div>

<div style="margin: 20px 0;">
  <h4>Misi</h4>
  <ul>
    <li style="margin-bottom: 8px;">Menghimpun dan menyajikan informasi terkini secara akurat dan bertanggung jawab.</li>
    <li style="margin-bottom: 8px;">Memberikan pengalaman membaca yang cepat dan nyaman di berbagai perangkat.</li>
    <li style="margin-bottom: 8px;">Menjaga transparansi sumber pada setiap konten yang disajikan.</li>
    <li style="margin-bottom: 8px;">Menghadirkan layanan konten premium bagi pembaca yang menginginkan akses informasi lebih mendalam melalui aplikasi keanggotaan.</li>
  </ul>
</div>

<hr style="margin: 30px 0; border: 0; border-top: 1px solid var(--border);" />

<h3>Legalitas Perusahaan</h3>
<p>Sebagai wujud kepatuhan terhadap regulasi hukum di Republik Indonesia, berikut adalah data legalitas badan hukum kami:</p>

<table class="table" style="width: 100%; margin-top: 20px; border-collapse: collapse;">
  <tbody>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="width: 35%; padding: 12px 8px; font-weight: bold;">Nama Perusahaan</td>
      <td style="padding: 12px 8px;">PT SIAP DIGITAL AGENCY</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Nomor Pokok Wajib Pajak (NPWP)</td>
      <td style="padding: 12px 8px;">Tersedia atas permintaan (keperluan administrasi)</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Surat Keterangan Terdaftar (SKT)</td>
      <td style="padding: 12px 8px;">Nomor: S-45199/SKT-WP-CT/KPP.2807/2026</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Tanggal Terdaftar</td>
      <td style="padding: 12px 8px;">02 Juli 2026</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Kantor Pelayanan Pajak (KPP)</td>
      <td style="padding: 12px 8px;">KPP Pratama Kotabumi</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Alamat Kantor / Badan Hukum</td>
      <td style="padding: 12px 8px;">Dusun Bumi Jaya, Pekon Bumi Hantatai, Bandar Negeri Suoh, Kab. Lampung Barat, Lampung, 34864</td>
    </tr>
  </tbody>
</table>')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value 
WHERE site_settings.value = '' OR site_settings.value NOT LIKE '%PT SIAP DIGITAL AGENCY%';

-- Populate Contact Page (page_contact_content)
INSERT INTO site_settings (key, value) VALUES
('page_contact_content', '<p class="lead">Kami sangat senang mendengar dari Anda. Apakah Anda memiliki saran, keluhan, informasi berita, atau penawaran kerja sama? Silakan hubungi kami melalui saluran resmi berikut:</p>

<div style="display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 30px; margin-top: 30px;">
  <div style="background: var(--light); padding: 20px; border-radius: 8px; border: 1px solid var(--border);">
    <h4 style="margin-top: 0; color: var(--red);">Redaksi</h4>
    <p style="font-size: 0.9em; margin-bottom: 15px;">Untuk koreksi berita, hak jawab, kiriman press release, atau tips berita:</p>
    <a href="mailto:redaksi@siapdigital.com" style="font-weight: bold; color: var(--text);">📧 redaksi@siapdigital.com</a>
  </div>
  
  <div style="background: var(--light); padding: 20px; border-radius: 8px; border: 1px solid var(--border);">
    <h4 style="margin-top: 0; color: var(--red);">Kerja Sama &amp; Periklanan</h4>
    <p style="font-size: 0.9em; margin-bottom: 15px;">Untuk pemasangan iklan banner, advertorial, atau program kerja sama komersial:</p>
    <a href="mailto:iklan@siapdigital.com" style="font-weight: bold; color: var(--text);">📧 iklan@siapdigital.com</a>
  </div>

  <div style="background: var(--light); padding: 20px; border-radius: 8px; border: 1px solid var(--border);">
    <h4 style="margin-top: 0; color: var(--red);">Layanan Pelanggan (CS)</h4>
    <p style="font-size: 0.9em; margin-bottom: 10px;">Untuk dukungan teknis pendaftaran akun, akses premium, atau aplikasi:</p>
    <p style="margin: 5px 0;"><a href="mailto:cs@siapdigital.com" style="font-weight: bold; color: var(--text);">📧 cs@siapdigital.com</a></p>
    <p style="margin: 5px 0; font-weight: bold;">📱 WhatsApp: +62 812-3456-7890</p>
    <p style="font-size: 0.8em; color: var(--text-muted); margin-top: 5px;">Jam layanan: Senin–Jumat, 09.00–17.00 WIB</p>
  </div>

  <div style="background: var(--light); padding: 20px; border-radius: 8px; border: 1px solid var(--border);">
    <h4 style="margin-top: 0; color: var(--red);">Kantor Pusat Perusahaan</h4>
    <p style="font-weight: bold; margin-bottom: 5px;">🏢 PT SIAP DIGITAL AGENCY</p>
    <p style="font-size: 0.9em; line-height: 1.5; margin: 0;">Dusun Bumi Jaya, Pekon Bumi Hantatai, Bandar Negeri Suoh, Kab. Lampung Barat, Lampung, 34864</p>
  </div>
</div>')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
WHERE site_settings.value = '' OR site_settings.value NOT LIKE '%redaksi@siapdigital.com%';

-- Populate Privacy Page (page_privacy_content)
INSERT INTO site_settings (key, value) VALUES
('page_privacy_content', '<p class="lead">Terakhir diperbarui: 03 Juli 2026</p>

<p><strong>PT SIAP DIGITAL AGENCY</strong> ("kami") mengelola situs <strong>Siap Digital News</strong> beserta aplikasi keanggotaan premium terkait ("Layanan"). Kebijakan ini menjelaskan bagaimana kami mengumpulkan, menggunakan, menyimpan, dan melindungi data pribadi Anda, sesuai dengan Undang-Undang Republik Indonesia Nomor 27 Tahun 2022 tentang Pelindungan Data Pribadi (UU PDP). Dengan mengakses situs atau layanan kami, Anda menyetujui praktik yang dijelaskan di bawah ini.</p>

<h3 style="margin-top: 30px;">1. Data yang Kami Kumpulkan</h3>
<ul>
  <li style="margin-bottom: 8px;"><strong>Data yang Anda berikan langsung:</strong> Nama lengkap, alamat email, dan nomor telepon saat melakukan pendaftaran akun premium; informasi pembayaran saat melakukan langganan (diproses secara aman oleh mitra payment gateway pihak ketiga — kami tidak menyimpan data kartu atau akun bank Anda); serta isi pesan yang dikirimkan via formulir kontak atau email.</li>
  <li style="margin-bottom: 8px;"><strong>Data yang dikumpulkan secara otomatis:</strong> Alamat IP, jenis perangkat dan peramban (browser), data cookie, serta rekaman aktivitas penelusuran halaman di situs kami.</li>
</ul>

<h3 style="margin-top: 30px;">2. Tujuan Penggunaan Data</h3>
<p>Kami memproses data pribadi Anda untuk tujuan berikut:</p>
<ul>
  <li style="margin-bottom: 8px;">Menyediakan dan mengelola akun keanggotaan premium Anda.</li>
  <li style="margin-bottom: 8px;">Memproses transaksi pembayaran langganan melalui mitra payment gateway resmi.</li>
  <li style="margin-bottom: 8px;">Memberikan layanan dukungan teknis dan merespons pertanyaan pelanggan.</li>
  <li style="margin-bottom: 8px;">Mengirimkan pembaruan informasi penting terkait status layanan Anda.</li>
  <li style="margin-bottom: 8px;">Meningkatkan keamanan dan mendeteksi adanya aktivitas penipuan atau penyalahgunaan.</li>
</ul>

<h3 style="margin-top: 30px;">3. Cookie dan Teknologi Pelacakan</h3>
<p>Situs kami menggunakan cookie untuk mempertahankan status login Anda, menganalisis performa lalu lintas situs, serta menyajikan personalisasi iklan. Kami bekerja sama dengan pihak ketiga (seperti Google AdSense) yang dapat menempatkan cookie eksternal untuk menampilkan iklan berbasis minat Anda.</p>

<h3 style="margin-top: 30px;">4. Hak-Hak Pemilik Data Pribadi</h3>
<p>Sesuai dengan UU PDP, Anda memiliki hak-hak berikut:</p>
<ul>
  <li style="margin-bottom: 8px;">Memperoleh kejelasan identitas dan meminta akses atas data pribadi Anda yang kami simpan.</li>
  <li style="margin-bottom: 8px;">Meminta pemutakhiran atau perbaikan kesalahan data pribadi agar lebih akurat.</li>
  <li style="margin-bottom: 8px;">Meminta penghapusan atau pemusnahan data pribadi Anda (selama tidak bertentangan dengan kewajiban hukum kami).</li>
  <li style="margin-bottom: 8px;">Menarik kembali persetujuan pemrosesan data pribadi Anda yang telah diberikan sebelumnya.</li>
</ul>
<p>Untuk mengajukan permohonan pemenuhan hak pemilik data pribadi Anda, hubungi kami melalui alamat email: <a href="mailto:privasi@siapdigital.com" style="font-weight: bold; color: var(--text);">privasi@siapdigital.com</a>.</p>')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
WHERE site_settings.value = '' OR site_settings.value NOT LIKE '%PT SIAP DIGITAL AGENCY%';

-- Populate Ads Page (page_ads_content)
INSERT INTO site_settings (key, value) VALUES
('page_ads_content', '<p class="lead"><strong>Siap Digital News</strong> menjangkau pembaca yang aktif mencari informasi terkini setiap hari. Jadikan brand atau bisnis Anda bagian dari perjalanan informasi mereka melalui berbagai opsi media periklanan kreatif yang kami tawarkan.</p>

<h3 style="margin-top: 30px;">Mengapa Beriklan Bersama Kami?</h3>
<ul>
  <li style="margin-bottom: 8px;"><strong>Jangkauan Aktif:</strong> Pembaca setia yang aktif berinteraksi dengan konten berita harian dan premium.</li>
  <li style="margin-bottom: 8px;"><strong>Format Fleksibel:</strong> Opsi penempatan iklan yang bervariasi dari display banner hingga advertorial kustom.</li>
  <li style="margin-bottom: 8px;"><strong>Integrasi Lintas Platform:</strong> Menjangkau audiens baik di situs web versi desktop/mobile maupun aplikasi mobile premium.</li>
</ul>

<h3 style="margin-top: 30px;">Format Iklan yang Tersedia</h3>
<table class="table" style="width: 100%; border-collapse: collapse; margin-top: 15px;">
  <thead>
    <tr style="border-bottom: 2px solid var(--red); background: var(--light);">
      <th style="padding: 12px 8px; text-align: left;">Format Iklan</th>
      <th style="padding: 12px 8px; text-align: left;">Deskripsi</th>
      <th style="padding: 12px 8px; text-align: left;">Dimensi / Ukuran</th>
    </tr>
  </thead>
  <tbody>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Leaderboard Banner</td>
      <td style="padding: 12px 8px;">Banner premium di bagian atas halaman utama dan artikel.</td>
      <td style="padding: 12px 8px;">728 × 90 px</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Medium Rectangle</td>
      <td style="padding: 12px 8px;">Banner yang diletakkan di tengah konten artikel atau sidebar.</td>
      <td style="padding: 12px 8px;">300 × 250 px</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Mobile Banner</td>
      <td style="padding: 12px 8px;">Banner responsif yang khusus disajikan pada layar smartphone.</td>
      <td style="padding: 12px 8px;">320 × 50 px</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Native Advertorial</td>
      <td style="padding: 12px 8px;">Artikel bersponsor yang ditulis sesuai dengan gaya penulisan redaksional (tetap berlabel "Advertorial").</td>
      <td style="padding: 12px 8px;">Kustom</td>
    </tr>
    <tr style="border-bottom: 1px solid var(--border);">
      <td style="padding: 12px 8px; font-weight: bold;">Push Notification</td>
      <td style="padding: 12px 8px;">Pemberitahuan singkat instan yang langsung dikirimkan ke aplikasi handphone para anggota premium.</td>
      <td style="padding: 12px 8px;">Kustom teks + tautan</td>
    </tr>
  </tbody>
</table>

<h3 style="margin-top: 30px;">Informasi Lebih Lanjut &amp; Kerjasama Iklan</h3>
<p>Untuk mendapatkan informasi Rate Card lengkap, analisis audiens, penawaran harga khusus (bundling), atau kerjasama strategis lainnya, silakan hubungi tim sales kami melalui:</p>
<p>📧 Email: <a href="mailto:iklan@siapdigital.com" style="font-weight: bold; color: var(--text);">iklan@siapdigital.com</a></p>')
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
WHERE site_settings.value = '' OR site_settings.value NOT LIKE '%iklan@siapdigital.com%';
