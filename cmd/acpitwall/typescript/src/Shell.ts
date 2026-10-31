// Shell wires up the app shell: the theme toggle in the top bar and the off-canvas sidebar on small screens.
export class Shell {
    public constructor() {
        $("[data-theme-toggle]").on("click", Shell.toggleTheme);
        $("[data-sidebar-toggle]").on("click", Shell.toggleSidebar);

        $(document).on("keydown", (e) => {
            if (e.key === "Escape" && $("body").hasClass("sidebar-open")) {
                Shell.toggleSidebar();
            }
        });
    }

    private static toggleTheme() {
        let root = document.documentElement;
        let theme = root.getAttribute("data-theme") === "dark" ? "light" : "dark";

        root.setAttribute("data-theme", theme);

        try {
            localStorage.setItem("theme", theme);
        } catch (e) {
            // storage can be unavailable (e.g. private mode), the theme then only applies to this page
        }
    }

    private static toggleSidebar() {
        let open = $("body").toggleClass("sidebar-open").hasClass("sidebar-open");

        $(".app-sidebar-toggle").attr("aria-expanded", open ? "true" : "false");
    }
}
