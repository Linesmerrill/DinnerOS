# ruby ios/fastlane/test/sdk_check_test.rb (macOS: needs zip, unzip and plutil).
require "minitest/autorun"
require "tmpdir"
require "fileutils"
require_relative "../sdk_check"

class SdkCheckTest < Minitest::Test
  def test_reads_the_major_version
    assert_equal 27, ios_sdk_major("iphoneos27.2")
    assert_equal 27, ios_sdk_major("iphoneos27")
    assert_equal 26, ios_sdk_major("iphoneos26.5")
    assert_equal 28, ios_sdk_major("iphoneos28.0.1")
  end

  def test_other_names_have_no_version
    assert_nil ios_sdk_major("iphonesimulator27.2")
    assert_nil ios_sdk_major("macosx27.0")
    assert_nil ios_sdk_major("")
    assert_nil ios_sdk_major(nil)
  end

  def test_an_ios_27_ipa_passes
    with_ipa("iphoneos27.2") { |ipa| assert_equal "iphoneos27.2", require_ios_sdk(ipa, 27) }
  end

  def test_an_ios_26_ipa_is_stopped
    with_ipa("iphoneos26.5") do |ipa|
      error = assert_raises(RuntimeError) { require_ios_sdk(ipa, 27) }
      assert_match(/iphoneos26\.5/, error.message)
    end
  end

  def test_a_missing_sdk_name_is_stopped
    with_ipa(nil) { |ipa| assert_raises(RuntimeError) { require_ios_sdk(ipa, 27) } }
  end

  def test_only_the_app_plist_counts
    # An extension built differently must not be read in place of the app.
    with_ipa("iphoneos26.5", extension_sdk: "iphoneos27.2") do |ipa|
      assert_raises(RuntimeError) { require_ios_sdk(ipa, 27) }
    end
  end

  private

  def with_ipa(sdk_name, extension_sdk: nil)
    Dir.mktmpdir do |dir|
      app = File.join(dir, "Payload", "DinnerOS.app")
      FileUtils.mkdir_p(app)
      write_plist(File.join(app, "Info.plist"), sdk_name)
      if extension_sdk
        ext = File.join(app, "PlugIns", "Share.appex")
        FileUtils.mkdir_p(ext)
        write_plist(File.join(ext, "Info.plist"), extension_sdk)
      end
      ipa = File.join(dir, "App.ipa")
      system("zip", "-qr", ipa, "Payload", chdir: dir) or raise "zip failed"
      yield ipa
    end
  end

  def write_plist(path, sdk_name)
    entry = sdk_name ? "<key>DTSDKName</key><string>#{sdk_name}</string>" : ""
    File.write(path, <<~PLIST)
      <?xml version="1.0" encoding="UTF-8"?>
      <!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
      <plist version="1.0"><dict><key>CFBundleName</key><string>DinnerOS</string>#{entry}</dict></plist>
    PLIST
  end
end
