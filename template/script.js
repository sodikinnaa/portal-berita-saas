/* 
================================================================
   INTERACTIVE SCRIPTS: AKUBANK YAPIS JAYAPURA TEMPLATE
================================================================
*/

document.addEventListener("DOMContentLoaded", () => {
    // 1. Dynamic Footer Year
    const yearEl = document.getElementById("current-year");
    if (yearEl) {
        yearEl.textContent = new Date().getFullYear();
    }

    // 2. Mobile Menu Toggle
    const menuToggle = document.getElementById("menu-toggle-btn");
    const mainNav = document.getElementById("main-navigation");

    if (menuToggle && mainNav) {
        menuToggle.addEventListener("click", () => {
            mainNav.classList.toggle("active");
            menuToggle.textContent = mainNav.classList.contains("active") ? "✕" : "☰";
        });

        // Close menu when clicking outside
        document.addEventListener("click", (e) => {
            if (!menuToggle.contains(e.target) && !mainNav.contains(e.target)) {
                mainNav.classList.remove("active");
                menuToggle.textContent = "☰";
            }
        });
    }

    // 3. Hero Slider
    const slides = document.querySelectorAll(".hero-slide");
    let currentSlide = 0;
    const slideInterval = 6000; // 6 seconds

    function nextSlide() {
        if (slides.length > 0) {
            slides[currentSlide].classList.remove("active");
            currentSlide = (currentSlide + 1) % slides.length;
            slides[currentSlide].classList.add("active");
        }
    }

    if (slides.length > 1) {
        setInterval(nextSlide, slideInterval);
    }

    // 4. Sticky Header on Scroll
    const header = document.querySelector(".main-header");
    if (header) {
        window.addEventListener("scroll", () => {
            if (window.scrollY > 50) {
                header.style.padding = "4px 0";
                header.style.boxShadow = "0 10px 15px -3px rgba(0, 0, 0, 0.1)";
            } else {
                header.style.padding = "0";
                header.style.boxShadow = "0 4px 6px -1px rgba(0,0,0,0.05)";
            }
        });
    }

    // 5. Stat Counter Animation
    const stats = document.querySelectorAll(".stat-number");
    const speed = 150; // lower number = faster count

    const startCounter = (el) => {
        const target = +el.getAttribute("data-target");
        const count = +el.innerText.replace(/[^\d]/g, '');
        const inc = Math.ceil(target / speed);

        if (count < target) {
            el.innerText = (count + inc) + (el.innerText.includes('%') ? '%' : el.innerText.includes('+') ? '+' : '');
            setTimeout(() => startCounter(el), 15);
        } else {
            el.innerText = target + (el.getAttribute("data-suffix") || "");
        }
    };

    // Intersection Observer to trigger counter when visible
    const observerOptions = {
        threshold: 0.5
    };

    const observer = new IntersectionObserver((entries, observer) => {
        entries.forEach(entry => {
            if (entry.isIntersecting) {
                startCounter(entry.target);
                observer.unobserve(entry.target);
            }
        });
    }, observerOptions);

    stats.forEach(stat => {
        observer.observe(stat);
    });
});
