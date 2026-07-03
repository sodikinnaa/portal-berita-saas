document.querySelectorAll('a[href="#"]').forEach((anchor) => {
    anchor.addEventListener("click", (event) =>
        event.preventDefault(),
    );
});

// Script untuk mobile text resizer
(function() {
    const toggleBtn = document.getElementById('resizerToggle');
    const panel = document.getElementById('resizerPanel');
    const slider = document.getElementById('fontSizeSlider');
    const display = document.getElementById('fontSizeDisplay');
    const decBtn = document.getElementById('decFont');
    const incBtn = document.getElementById('incFont');

    if (!toggleBtn || !panel || !slider || !display) return;

    // Load initial size from localStorage or use default 17
    let currentSize = parseInt(localStorage.getItem('article_font_size')) || 17;
    
    function updateFontSize(size) {
        if (size < 14) size = 14;
        if (size > 26) size = 26;
        currentSize = size;
        
        document.documentElement.style.setProperty('--article-font-size', size + 'px');
        
        slider.value = size;
        display.textContent = size + 'px';
        localStorage.setItem('article_font_size', size);
    }

    // Initial apply
    updateFontSize(currentSize);

    // Toggle panel visibility
    toggleBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        panel.classList.toggle('show');
    });

    // Close panel if click outside
    document.addEventListener('click', (e) => {
        if (!panel.contains(e.target) && !toggleBtn.contains(e.target)) {
            panel.classList.remove('show');
        }
    });

    // Slider input change
    slider.addEventListener('input', (e) => {
        updateFontSize(parseInt(e.target.value));
    });

    // Decrement button
    decBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        updateFontSize(currentSize - 1);
    });

    // Increment button
    incBtn.addEventListener('click', (e) => {
        e.stopPropagation();
        updateFontSize(currentSize + 1);
    });
})();

// Script untuk Table of Contents
(function() {
    const articleBody = document.querySelector('.article-body');
    if (!articleBody) return;

    const headings = articleBody.querySelectorAll('h1, h2, h3, h4');
    if (headings.length < 2) return;

    const tocContainer = document.createElement('div');
    tocContainer.className = 'toc-container';
    tocContainer.id = 'tocContainer';

    const tocHeader = document.createElement('div');
    tocHeader.className = 'toc-header';
    
    const tocTitle = document.createElement('h2');
    tocTitle.className = 'toc-title';
    tocTitle.innerHTML = `<svg xmlns="http://www.w3.org/2000/svg" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="8" y1="6" x2="21" y2="6"></line><line x1="8" y1="12" x2="21" y2="12"></line><line x1="8" y1="18" x2="21" y2="18"></line><line x1="3" y1="6" x2="3.01" y2="6"></line><line x1="3" y1="12" x2="3.01" y2="12"></line><line x1="3" y1="18" x2="3.01" y2="18"></line></svg> Daftar Isi`;
    
    const toggleBtn = document.createElement('button');
    toggleBtn.className = 'toc-toggle-btn';
    toggleBtn.innerHTML = `<span>Sembunyikan</span> <svg class="toc-toggle-icon" xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"></polyline></svg>`;
    
    tocHeader.appendChild(tocTitle);
    tocHeader.appendChild(toggleBtn);
    tocContainer.appendChild(tocHeader);

    const tocList = document.createElement('ol');
    tocList.className = 'toc-list';

    headings.forEach((heading, index) => {
        if (!heading.id) {
            const slug = heading.textContent
                .toLowerCase()
                .trim()
                .replace(/[^a-z0-9\s-]/g, '')
                .replace(/\s+/g, '-')
                .replace(/-+/g, '-');
            heading.id = slug || `heading-${index + 1}`;
        }

        const listItem = document.createElement('li');
        listItem.className = `toc-item level-${heading.tagName.toLowerCase()}`;
        
        const link = document.createElement('a');
        link.className = 'toc-link';
        link.href = `#${heading.id}`;
        link.textContent = heading.textContent;
        
        link.addEventListener('click', (e) => {
            e.preventDefault();
            const target = document.getElementById(heading.id);
            if (target) {
                target.scrollIntoView({ behavior: 'smooth' });
                history.pushState(null, null, `#${heading.id}`);
            }
        });

        listItem.appendChild(link);
        tocList.appendChild(listItem);
    });

    tocContainer.appendChild(tocList);

    // Sisipkan TOC di awal artikel body (sebelum paragraf pertama)
    articleBody.insertBefore(tocContainer, articleBody.firstChild);

    const toggleTOC = () => {
        tocContainer.classList.toggle('collapsed');
        const isCollapsed = tocContainer.classList.contains('collapsed');
        toggleBtn.querySelector('span').textContent = isCollapsed ? 'Tampilkan' : 'Sembunyikan';
    };

    tocHeader.addEventListener('click', toggleTOC);
})();

// Script untuk menyisipkan rekomendasi "Baca Juga" setelah paragraf 2 dan 4 secara otomatis
(function() {
    const articleBody = document.querySelector('.article-body');
    if (!articleBody) return;

    // Ambil semua paragraf langsung dalam .article-body
    const paragraphs = Array.from(articleBody.children).filter(el => el.tagName.toLowerCase() === 'p');
    if (paragraphs.length < 2) return;

    // Ambil daftar artikel populer dari data global yang diset oleh HTML template
    const relatedArticles = window.popularArticles || [];
    if (relatedArticles.length === 0) return;

    // Fungsi helper untuk membuat elemen box Baca Juga
    function createBacaJugaBox(article) {
        const box = document.createElement('div');
        box.className = 'baca-juga-box';
        
        const label = document.createElement('span');
        label.className = 'baca-juga-label';
        label.textContent = 'BACA JUGA:';
        
        const link = document.createElement('a');
        link.className = 'baca-juga-link';
        link.href = `/artikel/${article.slug}`;
        link.textContent = article.title;
        
        box.appendChild(label);
        box.appendChild(link);
        return box;
    }

    // Sisipkan setelah paragraf ke-2 (indeks 1 di list)
    if (paragraphs.length >= 2 && relatedArticles[0]) {
        const box1 = createBacaJugaBox(relatedArticles[0]);
        paragraphs[1].insertAdjacentElement('afterend', box1);
    }

    // Sisipkan setelah paragraf ke-4 (indeks 3 di list)
    if (paragraphs.length >= 4 && (relatedArticles[1] || relatedArticles[0])) {
        const box2 = createBacaJugaBox(relatedArticles[1] || relatedArticles[0]);
        paragraphs[3].insertAdjacentElement('afterend', box2);
    }
})();
