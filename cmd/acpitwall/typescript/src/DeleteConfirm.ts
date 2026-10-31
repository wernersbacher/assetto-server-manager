// DeleteConfirm replaces the browser confirm() popup for destructive actions. Any form marked up with
// data-confirm-delete="Delete X?" shows the shared modal in layout/base.html; the form is only submitted once the
// Delete button is pressed. Optional: data-confirm-detail (explanation) and data-confirm-label (button text).
// data-confirm-action="Stop X?" works the same for non-delete actions (stop, restart) and shows a neutral
// warning icon with a warning button instead of the red trash can.
export class DeleteConfirm {
    private pending: HTMLFormElement | null = null;

    public constructor() {
        const $modal = $("#delete-confirm-modal");

        if (!$modal.length) {
            return;
        }

        $(document).on("submit", "form[data-confirm-delete], form[data-confirm-action]", (e) => {
            const form = e.currentTarget as HTMLFormElement;

            if (form.dataset.confirmed === "true") {
                return; // confirmed already, let the submission through
            }

            e.preventDefault();
            e.stopPropagation();

            this.pending = form;

            const isAction = form.dataset.confirmAction !== undefined;
            const title = isAction ? form.dataset.confirmAction : form.dataset.confirmDelete;

            $modal.find(".delete-dialog").toggleClass("delete-dialog-action", isAction);
            $("#delete-confirm-ok").toggleClass("btn-warning", isAction).toggleClass("btn-danger", !isAction);

            $("#delete-confirm-title").text(title || (isAction ? "Are you sure?" : "Delete this item?"));
            $("#delete-confirm-detail").text(form.dataset.confirmDetail || (isAction ? "" : "This cannot be undone."));
            $("#delete-confirm-ok").text(form.dataset.confirmLabel || (isAction ? "Confirm" : "Delete"));

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
