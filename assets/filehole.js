const PublicUrl = "{{ .PublicUrl }}";

var entityMap = {
  "&": "&amp;",
  "<": "&lt;",
  ">": "&gt;",
  '"': "&quot;",
  "'": "&#39;",
  "/": "&#x2F;",
  "`": "&#x60;",
  "=": "&#x3D;",
};

function escapeHtml(string) {
  return String(string).replace(/[&<>"'`=\/]/g, function (s) {
    return entityMap[s];
  });
}

var uploading = false;

document.addEventListener("paste", function (event) {
  if (uploading) {
    return;
  }
  var clip = event.clipboardData;
  for (const item of clip.items) {
    if (item.kind === "file") {
      addToQueue(item.getAsFile());
    }
    if (item.kind === "string") {
      item.getAsString((s) => {
        addToQueue({
          name: "clipboard string",
          size: s.length,
          type: "text/plain",
          data: s,
          toString: () => {
            return s;
          },
        });
      });
    }
  }
});

$(function () {
  $("#upload-browse").show();
  $("#upload-browse").html(
    '<fieldset role="group" class="nomargin"><input type="file" id="uploadFiles" multiple="multiple"></input><input id="upload-start" type="submit" value="Upload"></input></fieldset>',
  );

  $("#uploadFiles").on("change", () => {
    var files = $("#uploadFiles")[0].files;

    for (var i = 0; i < files.length; i++) {
      addToQueue(files[i]);
    }
    $("#uploadFiles").val(undefined);
  });

  $("#urllen").on("change", () => {
    $("#urllen").val(Math.max(Math.min($("#urllen").val(), 236), 5));
  });

  // History
  if (localStorage.getItem("history_optin") === "true") {
    $("#history-optin").prop("checked", true);
  }

  var serializedFileHistory = localStorage.getItem("history") || "[]";
  fileHistory = JSON.parse(serializedFileHistory);

  $("#history-optin").on("change", (e) => {
    localStorage.setItem("history_optin", e.target.checked);
    renderFileHistory();
  });
  $("#file-history").show();
  // ---

  $("body").on("drop", (e) => {
    e.stopPropagation();
    e.preventDefault();

    if (uploading) {
      return;
    }

    for (const f of e.originalEvent.dataTransfer.files) {
      addToQueue(f);
    }
  });

  $("body").on("dragover", (e) => {
    e.stopPropagation();
    e.preventDefault();
    e.originalEvent.dataTransfer.dropEffect = "copy";
  });

  $("#upload-start").on("click", uploadFileQueue);

  renderFileHistory();
});

var fileQueue = [];
var fileHistory = [];

$(document).ready(function () {
  $("#clear-history").on("click", clearHistory);
});

function clearHistory() {
  fileHistory = [];
  localStorage.setItem("history", JSON.stringify(fileHistory));
  renderFileHistory();
}

function addToQueue(file) {
  fileQueue.push(file);
  renderFileQueue();
}

function removeFromQueue(index) {
  if (index > -1) {
    fileQueue.splice(index, 1);
  }
  renderFileQueue();
}

function uploadFileQueue() {
  uploading = true;
  $("#urllen").prop("disabled", true);
  $("#expiry").prop("disabled", true);
  $("#upload-start").prop("disabled", true);
  $("#upload-browse").hide();
  $(".delete-btn")
    .each(() => {})
    .remove();

  var uploadPromises = [];

  for (var index = 0; index < fileQueue.length; index++) {
    uploadPromises.push(uploadFile(index));
  }

  Promise.all(uploadPromises)
    .then((resps) => {
      console.log(resps);
    })
    .catch((error) => {
      console.log(error);
    })
    .finally(() => {
      $("#urllen").prop("disabled", false);
      $("#expiry").prop("disabled", false);
      $("#clear-queue").show();
    });
}

async function uploadFile(index) {
  return new Promise(function (resolve, reject) {
    var file = fileQueue[index];
    console.log("Uploading", index, file);
    var formData = new FormData();
    formData.append("file", file);
    formData.append("url_len", $("#urllen").val());
    formData.append("expiry", $("#expiry").val());
    $.ajax({
      url: PublicUrl,
      method: "POST",
      cache: false,
      data: formData,
      contentType: false, // jquery fucks the upload if you dont do this
      processData: false,
      xhr: () => {
        var xhr = new window.XMLHttpRequest();
        xhr.upload.onprogress = (e) => {
          if (e.lengthComputable) {
            var prog = Math.floor((e.loaded / e.total) * 100);
            updateProgress(index, prog);
          }
        };
        return xhr;
      },
      success: (resp) => {
        if (localStorage.getItem("history_optin") === "true") {
          var curDate = new Date();
          fileHistory.unshift({
            when: curDate,
            name: file.name,
            size: file.size,
            type: file.type,
            link: resp,
          });
          localStorage.setItem("history", JSON.stringify(fileHistory));
          renderFileHistory();
        }
        resolve(resp);
        replaceProgressWithLink(index, resp);
      },
      error: (r1, err) => {
        reject(err);
        replaceProgressWithError(index, err);
      },
    });
  });
}

function updateProgress(index, prog) {
  var fixedIndex = index.toString();
  $("#upload-prog-" + fixedIndex).attr("value", prog);
}

function replaceProgressWithLink(index, link) {
  var fixedIndex = index.toString();
  $("#upload-prog-" + fixedIndex).replaceWith(
    '<br><a href="' + link + '">' + link + "</a>",
  );
}

function replaceProgressWithError(index, error) {
  var fixedIndex = index.toString();
  $("#upload-prog-" + fixedIndex).replaceWith("<p>" + error + "</p>");
}

function renderFileHistory() {
  var fileHistoryHtml = "<section>";

  fileHistory.forEach((file, index) => {
    fileHistoryHtml +=
      "<p><b>" +
      escapeHtml(new Date(file.when).toLocaleDateString()) +
      " " +
      escapeHtml(new Date(file.when).toLocaleTimeString()) +
      "</b></p>" +
      "<p>" +
      escapeHtml(file.name) +
      " (" +
      escapeHtml(formatBytes(file.size)) +
      ") <small>" +
      escapeHtml(file.type) +
      "</small></span>";
    fileHistoryHtml +=
      '<br><a href="' +
      escapeHtml(file.link) +
      '">' +
      escapeHtml(file.link) +
      "</a>";
    if (index != fileHistory.length - 1) {
      fileHistoryHtml += "<hr>";
    }
  });

  fileHistoryHtml += "</section>";

  $("#file-history-list").html(fileHistoryHtml);
}

function renderFileQueue() {
  fq = $("#file-queue");
  fq.show();
  fq.empty();

  fq.append("<section><b>Queue</b></section>");

  fileQueueSection = $("<section></section>");

  fileQueue.forEach((file, index) => {
    fileQueueSection.append(
      '<span class="filedetails">' +
        escapeHtml(file.name) +
        " (" +
        escapeHtml(formatBytes(file.size)) +
        ") <small>" +
        escapeHtml(file.type) +
        "</small></span>",
    );
    x = $(
      '<span class="delete-btn"><a class="pico-color-red-450">x</a></span>',
    );
    x.on("click", function () {
      removeFromQueue(index);
    });
    fileQueueSection.append(x);

    fileQueueSection.append(
      '<progress id="upload-prog-' +
        escapeHtml(index.toString()) +
        '" value="0" max="100"></progress>',
    );
    if (index != fileQueue.length - 1) {
      fileQueueSection.append("<hr>");
    }
  });

  fq.append(fileQueueSection);

  clearQueueSection = $("<section></section>")
    .attr("id", "clear-queue")
    .addClass("needs-javascript")
    .addClass("nomargin");

  clearQueueBtn = $("<button></button>");
  clearQueueBtn.on("click", function () {
    fileQueue = [];
    $("#upload-start").prop("disabled", false);
    $("#upload-browse").show();
    uploading = false;
    $("#file-queue").hide();
    clearQueueBtn.hide();
  });

  clearQueueBtn.text("More");
  clearQueueSection.append(clearQueueBtn);

  fq.append(clearQueueSection);
}

function clearQueue() {}

function formatBytes(bytes, decimals) {
  if (bytes == 0) return "0 bytes";
  var k = 1024,
    dm = decimals || 2,
    sizes = ["bytes", "KiB", "MiB", "GiB", "TiB"],
    i = Math.floor(Math.log(bytes) / Math.log(k));
  return parseFloat((bytes / Math.pow(k, i)).toFixed(dm)) + " " + sizes[i];
}
