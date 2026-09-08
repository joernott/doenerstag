// The image upload control.
//
// One control for a restaurant logo and for a menu item's picture. It uploads
// immediately on selection and reports the new image's id, because an image is
// a resource of its own (ADR-0008): the form that owns it stores the id, and
// two forms pointing at the same picture share the stored bytes.

import { api, errorMessage, type VersionInfo } from "../api";
import { el, icon } from "../dom";
import { formatBytes } from "../format";
import type { Translator } from "../i18n";
import { button } from "./forms";

/** What POST /images answers with. */
interface UploadedImage {
  id: string;
  media_type: string;
  width: number;
  height: number;
  byte_size: number;
}

export interface ImageFieldOptions {
  t: Translator;
  /** The interface locale, for the size in the "too large" message. */
  locale: string;
  /** The visible label. */
  label: string;
  /** The image currently attached, if any. */
  imageId: string | null;
  /** What the server says it will accept, from /version. */
  limits: VersionInfo | null;
  /** Called after an upload or a removal. */
  onChange: (imageId: string | null) => void;
}

export interface ImageField {
  element: HTMLElement;
  /** The id currently chosen. */
  value(): string | null;
}

/** The URL of an image's thumbnail. */
export function thumbnailURL(imageId: string): string {
  return `/api/v1/images/${encodeURIComponent(imageId)}/thumbnail`;
}

/** The URL of an image at full size. */
export function imageURL(imageId: string): string {
  return `/api/v1/images/${encodeURIComponent(imageId)}`;
}

/** Builds the control. */
export function imageField(options: ImageFieldOptions): ImageField {
  const { t } = options;
  let current = options.imageId;

  const preview = el("div", { class: "image-preview" });
  const status = el("p", { class: "field-hint" });
  // The file input itself is never the control a person operates: the button
  // below opens it. It is therefore taken out of the tab order -- a focusable
  // element nobody can see cannot show a focus ring, and docs/06_ui_ux.md asks
  // for one everywhere -- and it still carries a name, because it is a form
  // element and an unnamed one is a violation whether or not anybody reaches
  // it.
  const file = el("input", {
    type: "file",
    class: "visually-hidden",
    accept: "image/jpeg,image/png,image/gif",
    tabindex: "-1",
    "aria-label": options.label,
  });

  const choose = button({
    label: current ? t.t("image.replace") : t.t("image.choose"),
    variant: "primary",
    onclick: () => file.click(),
  });
  const remove = button({
    label: t.t("image.remove"),
    variant: "danger",
    onclick: () => {
      current = null;
      options.onChange(null);
      render();
    },
  });

  function render(): void {
    preview.replaceChildren();
    if (current) {
      preview.appendChild(
        el("img", {
          class: "image-thumb",
          src: thumbnailURL(current),
          alt: t.t("image.current"),
          loading: "lazy",
        }),
      );
    } else {
      preview.appendChild(el("div", { class: "image-placeholder" }, icon("plus")));
    }
    choose.textContent = current ? t.t("image.replace") : t.t("image.choose");
    remove.hidden = current === null;
  }

  file.addEventListener("change", () => {
    const chosen = file.files?.[0];
    if (!chosen) {
      return;
    }
    void upload(chosen);
  });

  async function upload(chosen: File): Promise<void> {
    status.classList.remove("field-error");

    // Refused here rather than after a minute of uploading. The server checks
    // both again -- a limit the browser enforces is a courtesy, not a control
    // -- and answers 1011 and 1012 for these two cases.
    const limit = options.limits?.max_image_size ?? 0;
    if (limit > 0 && chosen.size > limit) {
      status.classList.add("field-error");
      status.textContent = t.t("image.too_large", {
        size: formatBytes(options.locale, chosen.size),
        limit: formatBytes(options.locale, limit),
      });
      file.value = "";
      return;
    }
    if (!chosen.type.startsWith("image/")) {
      status.classList.add("field-error");
      status.textContent = t.t("image.not_an_image");
      file.value = "";
      return;
    }

    status.textContent = t.t("image.uploading");
    choose.disabled = true;

    const body = new FormData();
    body.append("file", chosen);

    try {
      const uploaded = await api.post<UploadedImage>("/images", body);
      current = uploaded.id;
      options.onChange(uploaded.id);
      status.textContent = formatBytes(options.locale, uploaded.byte_size);
      render();
    } catch (error) {
      status.classList.add("field-error");
      status.textContent = errorMessage(t, error);
    } finally {
      choose.disabled = false;
      // Cleared so that choosing the same file again fires another change
      // event. Without this, an upload that failed cannot be retried.
      file.value = "";
    }
  }

  render();

  const element = el(
    "div",
    { class: "field image-field" },
    el("span", { class: "field-label", text: options.label }),
    el("div", { class: "image-row" }, preview, el("div", { class: "actions" }, choose, remove)),
    file,
    status,
  );

  return { element, value: () => current };
}
