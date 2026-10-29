// DeleteConfirm replaces the browser confirm() popup for destructive actions. Any form marked up with
// data-confirm-delete="Delete X?" shows the shared modal in layout/base.html; the form is only submitted once the
// Delete button is pressed. Optional: data-confirm-detail (explanation) and data-confirm-label (button text).
export class DeleteConfirm {
    private pending: HTMLFormElement | null = null;

    public constructor() {
        const $modal = $("#delete-confirm-modal");

        if (!$modal.length) {
            return;
        }

        $(document).on("submit", "form[data-confirm-delete]", (e) => {
            const form = e.currentTarget as HTMLFormElement;

            if (form.dataset.confirmed === "true") {
                return; // confirmed already, let the submission through
            }

            e.preventDefault();
            e.stopPropagation();

            this.pending = form;

            $("#delete-confirm-title").text(form.dataset.confirmDelete || "Delete this item?");
            $("#delete-confirm-detail").text(form.dataset.confirmDetail || "This cannot be undone.");
            $("#delete-confirm-ok").text(form.dataset.confirmLabel || "Delete");

            $modal.modal("show");
        });

        $("#delete-confirm-ok").on("click", () => {
            const form = this.pending;

            this.pending = null;
            $modal.modal("hide");

            if (form) {
                form.dataset.confirmed = "true";
                form.submit();
            }
        });

        // focus the safe choice, so Enter/Space never deletes by accident
        $modal.on("shown.bs.modal", () => $("#delete-confirm-cancel").trigger("focus"));
        $modal.on("hidden.bs.modal", () => this.pending = null);
    }
}
